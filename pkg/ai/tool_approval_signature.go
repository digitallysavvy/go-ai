package ai

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// toolApprovalPayloadVersion is the domain-separation prefix of the injective
// approval payload. Mirrors TS buildPayload in tool-approval-signature.ts.
const toolApprovalPayloadVersion = "ai-sdk-tool-approval-v1"

// InvalidToolApprovalSignatureError is returned when a client-supplied tool
// approval cannot be verified against the server-issued approval request.
type InvalidToolApprovalSignatureError struct {
	ApprovalID string
	ToolCallID string
	Reason     string
}

func (e *InvalidToolApprovalSignatureError) Error() string {
	return fmt.Sprintf(`Tool approval signature verification failed for approval "%s" (tool call "%s"): %s`, e.ApprovalID, e.ToolCallID, e.Reason)
}

// IsInvalidToolApprovalSignatureError reports whether err is an
// InvalidToolApprovalSignatureError. This is the Go equivalent of the
// TypeScript SDK's InvalidToolApprovalSignatureError.isInstance helper.
func IsInvalidToolApprovalSignatureError(err error) bool {
	var target *InvalidToolApprovalSignatureError
	return errors.As(err, &target)
}

// SignToolApproval signs a tool approval request with HMAC-SHA256.
//
// The signed payload is the injective JSON array
// ["ai-sdk-tool-approval-v1", approvalId, toolCallId, toolName, inputDigest],
// where inputDigest is the base64url SHA-256 digest of the canonical JSON
// input. The bytes are identical to the TypeScript SDK's JSON.stringify
// output, so signatures verify across both SDKs.
func SignToolApproval(secret []byte, approvalID, toolCallID, toolName string, input interface{}) (string, error) {
	inputDigest, err := hashCanonical(input)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(buildToolApprovalPayload(approvalID, toolCallID, toolName, inputDigest))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VerifyToolApprovalSignature verifies a signature produced by SignToolApproval.
//
// For backwards compatibility it also accepts signatures over the legacy
// newline-joined payload, but only when none of approvalID, toolCallID and
// toolName contain a newline (the condition that made the legacy format
// ambiguous). Mirrors TS verifyToolApprovalSignature.
func VerifyToolApprovalSignature(secret []byte, signature, approvalID, toolCallID, toolName string, input interface{}) (bool, error) {
	got, err := decodeBase64URLLenient(signature)
	if err != nil {
		return false, err
	}
	inputDigest, err := hashCanonical(input)
	if err != nil {
		return false, err
	}
	if hmac.Equal(got, hmacSHA256(secret, buildToolApprovalPayload(approvalID, toolCallID, toolName, inputDigest))) {
		return true, nil
	}
	if !strings.Contains(approvalID, "\n") &&
		!strings.Contains(toolCallID, "\n") &&
		!strings.Contains(toolName, "\n") {
		legacy := []byte(approvalID + "\n" + toolCallID + "\n" + toolName + "\n" + inputDigest)
		return hmac.Equal(got, hmacSHA256(secret, legacy)), nil
	}
	return false, nil
}

func hmacSHA256(secret, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

// buildToolApprovalPayload returns the bytes of
// JSON.stringify(['ai-sdk-tool-approval-v1', approvalId, toolCallId, toolName, inputDigest]).
func buildToolApprovalPayload(approvalID, toolCallID, toolName, inputDigest string) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, field := range []string{toolApprovalPayloadVersion, approvalID, toolCallID, toolName, inputDigest} {
		if i > 0 {
			buf.WriteByte(',')
		}
		writeJSString(&buf, field)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// decodeBase64URLLenient mirrors TS convertBase64ToUint8Array: it accepts
// base64url or standard base64 with or without padding.
func decodeBase64URLLenient(s string) ([]byte, error) {
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}

// hashCanonical returns the base64url SHA-256 digest of the canonical JSON of
// value. Mirrors TS hashCanonical in util/canonical-hash.ts.
func hashCanonical(value interface{}) (string, error) {
	canonical, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

// canonicalJSON serializes value with sorted object keys. The output is
// byte-identical to TS canonicalJSON (JSON.stringify for primitives), which
// means strings are NOT HTML-escaped and U+2028/U+2029 are NOT escaped.
func canonicalJSON(value interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonicalJSON(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonicalJSON(buf *bytes.Buffer, value interface{}) error {
	switch v := value.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case string:
		writeJSString(buf, v)
		return nil
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case float64:
		writeJSNumber(buf, v)
		return nil
	case float32:
		writeJSNumber(buf, float64(v))
		return nil
	case int:
		writeJSNumber(buf, float64(v))
		return nil
	case int8:
		writeJSNumber(buf, float64(v))
		return nil
	case int16:
		writeJSNumber(buf, float64(v))
		return nil
	case int32:
		writeJSNumber(buf, float64(v))
		return nil
	case int64:
		writeJSNumber(buf, float64(v))
		return nil
	case uint:
		writeJSNumber(buf, float64(v))
		return nil
	case uint8:
		writeJSNumber(buf, float64(v))
		return nil
	case uint16:
		writeJSNumber(buf, float64(v))
		return nil
	case uint32:
		writeJSNumber(buf, float64(v))
		return nil
	case uint64:
		writeJSNumber(buf, float64(v))
		return nil
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return err
		}
		writeJSNumber(buf, f)
		return nil
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		// JS Array.prototype.sort compares UTF-16 code units; Go compares
		// bytes. They agree except for supplementary-plane vs U+E000..U+FFFF
		// ordering, so sort by UTF-16 to stay byte-identical.
		sort.Slice(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSString(buf, key)
			buf.WriteByte(':')
			if err := writeCanonicalJSON(buf, v[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case []interface{}:
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonicalJSON(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case json.RawMessage:
		var decoded interface{}
		if err := json.Unmarshal(v, &decoded); err != nil {
			return err
		}
		return writeCanonicalJSON(buf, decoded)
	default:
		rv := reflect.ValueOf(value)
		if (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface || rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice) && rv.IsNil() {
			buf.WriteString("null")
			return nil
		}
		// Structs, typed maps and slices: normalize through encoding/json to
		// the generic JSON value model, then canonicalize.
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var decoded interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		return writeCanonicalJSON(buf, decoded)
	}
}

// writeJSNumber formats f like JavaScript Number#toString (used by
// JSON.stringify). Non-finite numbers serialize as null.
func writeJSNumber(buf *bytes.Buffer, f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		buf.WriteString("null")
		return
	}
	if f == 0 {
		buf.WriteByte('0') // JSON.stringify(-0) === "0"
		return
	}
	abs := math.Abs(f)
	if abs >= 1e21 || abs < 1e-6 {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		// Go: 1e+21, 1e-07; JS: 1e+21, 1e-7.
		mantissa, exp, _ := strings.Cut(s, "e")
		sign := exp[0]
		digits := strings.TrimLeft(exp[1:], "0")
		if digits == "" {
			digits = "0"
		}
		buf.WriteString(mantissa)
		buf.WriteByte('e')
		buf.WriteByte(sign)
		buf.WriteString(digits)
		return
	}
	buf.WriteString(strconv.FormatFloat(f, 'f', -1, 64))
}

const lowerHex = "0123456789abcdef"

// writeJSString writes s exactly as JavaScript JSON.stringify would: only
// '"', '\\' and control characters are escaped; HTML characters and
// U+2028/U+2029 are written verbatim. Invalid UTF-8 is replaced with U+FFFD
// (JS strings cannot contain invalid code units once encoded as UTF-8).
func writeJSString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			switch b {
			case '"':
				buf.WriteString(`\"`)
			case '\\':
				buf.WriteString(`\\`)
			case '\b':
				buf.WriteString(`\b`)
			case '\f':
				buf.WriteString(`\f`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				if b < 0x20 {
					buf.WriteString(`\u00`)
					buf.WriteByte(lowerHex[b>>4])
					buf.WriteByte(lowerHex[b&0xF])
				} else {
					buf.WriteByte(b)
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			buf.WriteString("�")
		} else {
			buf.WriteString(s[i : i+size])
		}
		i += size
	}
	buf.WriteByte('"')
}

// lessUTF16 compares strings by UTF-16 code units (JavaScript default sort).
func lessUTF16(a, b string) bool {
	ai, bi := 0, 0
	var aPending, bPending uint16
	for {
		var ca, cb uint16
		var aok, bok bool
		ca, aPending, ai, aok = nextUTF16Unit(a, ai, aPending)
		cb, bPending, bi, bok = nextUTF16Unit(b, bi, bPending)
		if !aok || !bok {
			return !aok && bok
		}
		if ca != cb {
			return ca < cb
		}
	}
}

func nextUTF16Unit(s string, i int, pending uint16) (unit uint16, nextPending uint16, next int, ok bool) {
	if pending != 0 {
		return pending, 0, i, true
	}
	if i >= len(s) {
		return 0, 0, i, false
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	if r >= 0x10000 {
		r -= 0x10000
		return uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF)), i + size, true
	}
	return uint16(r), 0, i + size, true
}
