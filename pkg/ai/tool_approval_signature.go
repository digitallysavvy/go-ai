package ai

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// InvalidToolApprovalSignatureError is returned when a client-supplied tool
// approval cannot be verified against the server-issued approval request.
type InvalidToolApprovalSignatureError struct {
	ApprovalID string
	ToolCallID string
	Reason     string
}

func (e *InvalidToolApprovalSignatureError) Error() string {
	return fmt.Sprintf("Tool approval signature verification failed for approval %q (tool call %q): %s", e.ApprovalID, e.ToolCallID, e.Reason)
}

// IsInvalidToolApprovalSignatureError reports whether err is an
// InvalidToolApprovalSignatureError. This is the Go equivalent of the
// TypeScript SDK's InvalidToolApprovalSignatureError.isInstance helper.
func IsInvalidToolApprovalSignatureError(err error) bool {
	var target *InvalidToolApprovalSignatureError
	return errors.As(err, &target)
}

// SignToolApproval signs the stable approval payload used by the TypeScript SDK:
// approval ID, tool call ID, tool name, and a SHA-256 digest of canonical JSON
// input, separated by newlines.
func SignToolApproval(secret []byte, approvalID, toolCallID, toolName string, input interface{}) (string, error) {
	payload, err := toolApprovalPayload(approvalID, toolCallID, toolName, input)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VerifyToolApprovalSignature verifies a signature produced by SignToolApproval.
func VerifyToolApprovalSignature(secret []byte, signature, approvalID, toolCallID, toolName string, input interface{}) (bool, error) {
	expected, err := SignToolApproval(secret, approvalID, toolCallID, toolName, input)
	if err != nil {
		return false, err
	}
	got, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false, err
	}
	want, err := base64.RawURLEncoding.DecodeString(expected)
	if err != nil {
		return false, err
	}
	return hmac.Equal(got, want), nil
}

func toolApprovalPayload(approvalID, toolCallID, toolName string, input interface{}) ([]byte, error) {
	canonical, err := canonicalJSON(input)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	inputDigest := base64.RawURLEncoding.EncodeToString(digest[:])
	return []byte(approvalID + "\n" + toolCallID + "\n" + toolName + "\n" + inputDigest), nil
}

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
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyJSON, _ := json.Marshal(key)
			buf.Write(keyJSON)
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
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(data)
		return nil
	}
}
