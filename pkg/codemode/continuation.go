package codemode

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// signatureAlgorithm is the value stored in ContinuationAuth.Alg. Mirrors
// TypeScript's SIGNATURE_ALGORITHM (code-mode/src/continuation-capability.ts).
const signatureAlgorithm = "HMAC-SHA256"

// defaultContinuationMaxAge mirrors TypeScript's DEFAULT_MAX_AGE_MS (1 hour).
const defaultContinuationMaxAge = time.Hour

var continuationSigningKeyMu sync.RWMutex

// defaultSigningKey/defaultMaxAge are the process-wide continuation signing
// defaults, mirroring TypeScript's module-level `defaultSigningKey`/
// `defaultMaxAgeMs` (code-mode/src/continuation-capability.ts). Randomly
// generated at first use so an unconfigured process still signs and
// verifies consistently within itself; set explicitly with
// SetCodeModeContinuationSigningKey.
var (
	defaultSigningKey []byte
	defaultMaxAge     = defaultContinuationMaxAge
)

func init() {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Never fall back to a predictable key: anyone who knew it could
		// forge continuations. (Since Go 1.24 crypto/rand.Read does not
		// return errors, so this is unreachable in practice.)
		panic("codemode: crypto/rand unavailable for the continuation signing key: " + err.Error())
	}
	defaultSigningKey = key
}

// SetCodeModeContinuationSigningKey sets the process-wide default
// continuation signing key and max age used whenever a call's
// Options.ContinuationSecurity / ContinuationSecurityOptions doesn't
// override them. key == nil generates a fresh random 32-byte key (as at
// process start); a non-nil, empty key is an error. maxAge == 0 keeps the
// current default (1 hour, unless already changed). Mirrors TypeScript's
// experimental_setCodeModeContinuationSigningKey.
//
// This mutates global state shared by every concurrent invocation in the
// process, exactly as TypeScript's module-level defaults are shared by
// every invocation in a JS realm; call it during setup, not from
// concurrent request handling.
func SetCodeModeContinuationSigningKey(key []byte, maxAge time.Duration) error {
	resolved, err := resolveContinuationSecurity(ContinuationSecurityOptions{SigningKey: key, MaxAge: maxAge})
	if err != nil {
		return err
	}
	continuationSigningKeyMu.Lock()
	defer continuationSigningKeyMu.Unlock()
	defaultSigningKey = resolved.signingKey
	defaultMaxAge = resolved.maxAge
	return nil
}

type resolvedContinuationSecurity struct {
	signingKey []byte
	maxAge     time.Duration
}

// resolveContinuationSecurity mirrors TypeScript's
// resolveCodeModeContinuationSecurity.
func resolveContinuationSecurity(opts ContinuationSecurityOptions) (resolvedContinuationSecurity, error) {
	key := opts.SigningKey
	if key == nil {
		continuationSigningKeyMu.RLock()
		key = append([]byte(nil), defaultSigningKey...)
		continuationSigningKeyMu.RUnlock()
	}
	if len(key) == 0 {
		return resolvedContinuationSecurity{}, fmt.Errorf("codemode: continuation signing key must not be empty")
	}

	maxAge := opts.MaxAge
	if maxAge == 0 {
		continuationSigningKeyMu.RLock()
		maxAge = defaultMaxAge
		continuationSigningKeyMu.RUnlock()
	}
	if maxAge <= 0 {
		return resolvedContinuationSecurity{}, fmt.Errorf("codemode: continuation maxAge must be a positive duration")
	}
	return resolvedContinuationSecurity{signingKey: key, maxAge: maxAge}, nil
}

// signContinuation signs an unsigned continuation envelope, producing its
// ContinuationAuth. Mirrors TypeScript's signCodeModeContinuation.
func signContinuation(unsigned Continuation, security resolvedContinuationSecurity) (Continuation, error) {
	issuedAt := time.Now().UnixMilli()
	nonce, err := randomHex(16)
	if err != nil {
		return Continuation{}, err
	}
	auth := ContinuationAuth{
		Alg:         signatureAlgorithm,
		Nonce:       nonce,
		IssuedAtMs:  issuedAt,
		ExpiresAtMs: issuedAt + security.maxAge.Milliseconds(),
	}

	signed := unsigned
	signed.Auth = auth
	signature, err := signContinuationPayload(signed, security.signingKey)
	if err != nil {
		return Continuation{}, err
	}
	signed.Auth.Signature = signature
	return signed, nil
}

// verifyContinuation validates a Continuation's shape, expiry and
// signature. Mirrors TypeScript's verifyCodeModeContinuation.
func verifyContinuation(continuation Continuation, opts ContinuationSecurityOptions) error {
	if continuation.Version != 2 ||
		continuation.ToolNames == nil ||
		continuation.Token == "" ||
		len(continuation.PendingInterruptions) == 0 ||
		continuation.Resolutions == nil {
		return NewProtocolError("Code mode continuation envelope is malformed.", nil)
	}
	if err := assertAuthShape(continuation.Auth); err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	if continuation.Auth.ExpiresAtMs < now {
		return NewProtocolError("Code mode continuation has expired.", map[string]interface{}{
			"expiresAtMs": continuation.Auth.ExpiresAtMs,
			"now":         now,
		})
	}
	if continuation.Auth.IssuedAtMs > now+60_000 {
		return NewProtocolError("Code mode continuation was issued in the future.", map[string]interface{}{
			"issuedAtMs": continuation.Auth.IssuedAtMs,
			"now":        now,
		})
	}

	security, err := resolveContinuationSecurity(opts)
	if err != nil {
		return err
	}
	stripped := continuation
	signature := stripped.Auth.Signature
	stripped.Auth.Signature = ""
	expected, err := signContinuationPayload(stripped, security.signingKey)
	if err != nil {
		return err
	}
	if !constantTimeEqual(signature, expected) {
		return NewProtocolError("Code mode continuation signature is invalid.", nil)
	}
	return nil
}

// hasValidContinuationCapability reports whether verifyContinuation
// succeeds, mirroring TypeScript's hasValidCodeModeContinuationCapability.
func hasValidContinuationCapability(continuation Continuation, opts ContinuationSecurityOptions) bool {
	return verifyContinuation(continuation, opts) == nil
}

// assertAuthShape mirrors TypeScript's assertAuthShape.
func assertAuthShape(auth ContinuationAuth) error {
	if auth.Alg != signatureAlgorithm ||
		len(auth.Nonce) != 32 || !isLowerHex(auth.Nonce) ||
		auth.ExpiresAtMs <= auth.IssuedAtMs ||
		auth.Signature == "" {
		return NewProtocolError("Code mode continuation is missing valid signed auth metadata.", nil)
	}
	return nil
}

func isLowerHex(s string) bool {
	if _, err := hex.DecodeString(s); err != nil {
		return false
	}
	return true
}

// signContinuationPayload computes the HMAC-SHA256 signature over the
// canonical JSON of continuation with its auth.signature blanked, mirroring
// TypeScript's signContinuationPayload (createHmac('sha256',
// key).update(canonicalJson(payload)).digest('base64url')).
func signContinuationPayload(continuation Continuation, signingKey []byte) (string, error) {
	unsigned := continuation
	unsigned.Auth.Signature = ""
	payload, err := canonicalJSON(unsigned)
	if err != nil {
		return "", NewProtocolError(fmt.Sprintf("Failed to canonicalize continuation for signing: %s", err.Error()), nil)
	}
	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", NewError("Failed to generate a continuation nonce.", "CODE_MODE_PROTOCOL_ERROR", nil)
	}
	return hex.EncodeToString(buf), nil
}

// canonicalJSON encodes value the way TypeScript's canonicalJson does:
// object keys sorted lexicographically at every level, no whitespace, and
// (unlike encoding/json.Marshal's default) no HTML-escaping of '<', '>' and
// '&'. value is round-tripped through encoding/json first so nested Go
// structs become map[string]interface{} (whose keys encoding/json already
// sorts) and every numeric Go type collapses to float64, matching
// JavaScript's single Number type. One narrow, accepted divergence from
// true JSON.stringify: encoding/json always escapes U+2028/U+2029 inside
// strings (a JS-safety measure with no opt-out), while JSON.stringify does
// not; this only affects continuation fields containing those rare
// line/paragraph-separator code points.
func canonicalJSON(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var generic interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
