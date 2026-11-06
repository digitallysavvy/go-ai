package codemode

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
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
// override them. key == nil generates a fresh random 32-byte key (rotating
// the key, which invalidates continuations signed with the old one); a
// non-nil, empty key is an error. maxAge == 0 resets the default to 1 hour,
// like TS (`options.maxAgeMs ?? DEFAULT_MAX_AGE_MS`). Mirrors TypeScript's
// experimental_setCodeModeContinuationSigningKey.
//
// This mutates global state shared by every concurrent invocation in the
// process, exactly as TypeScript's module-level defaults are shared by
// every invocation in a JS realm; call it during setup, not from
// concurrent request handling.
func SetCodeModeContinuationSigningKey(key []byte, maxAge time.Duration) error {
	if key == nil {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return fmt.Errorf("codemode: crypto/rand unavailable for the continuation signing key: %w", err)
		}
	}
	if maxAge == 0 {
		maxAge = defaultContinuationMaxAge
	}
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
// object keys sorted with String.prototype.localeCompare (ICU root collation,
// not code-unit order, so "a" < "B" < "c" and "_" < "a") at every level, no
// whitespace, and JSON.stringify's string escaping (no HTML-escaping of '<',
// '>' and '&', and U+2028/U+2029 written as is). value is round-tripped
// through encoding/json first so nested Go structs become
// map[string]interface{} and every numeric Go type collapses to float64,
// matching JavaScript's single Number type.
func canonicalJSON(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var generic interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := writeCanonical(&sb, generic, collate.New(language.Und)); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func writeCanonical(sb *strings.Builder, v interface{}, c *collate.Collator) error {
	switch t := v.(type) {
	case []interface{}:
		sb.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := writeCanonical(sb, item, c); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if r := c.CompareString(keys[i], keys[j]); r != 0 {
				return r < 0
			}
			return keys[i] < keys[j]
		})
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, err := jsonstr.Marshal(k)
			if err != nil {
				return err
			}
			sb.Write(kb)
			sb.WriteByte(':')
			if err := writeCanonical(sb, t[k], c); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	default:
		b, err := jsonstr.Marshal(v)
		if err != nil {
			return err
		}
		sb.Write(b)
	}
	return nil
}
