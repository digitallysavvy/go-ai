package klingai

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGenerateJWTToken(t *testing.T) {
	t.Run("generates valid JWT token structure", func(t *testing.T) {
		token, err := generateJWTToken("test-access-key", "test-secret-key")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// JWT should have 3 parts separated by dots
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			t.Errorf("expected 3 parts, got %d", len(parts))
		}
	})

	t.Run("includes correct header with HS256 algorithm", func(t *testing.T) {
		token, err := generateJWTToken("test-access-key", "test-secret-key")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		parts := strings.Split(token, ".")
		headerJSON, err := base64urlDecode(parts[0])
		if err != nil {
			t.Fatalf("failed to decode header: %v", err)
		}

		var header map[string]string
		if err := json.Unmarshal(headerJSON, &header); err != nil {
			t.Fatalf("failed to unmarshal header: %v", err)
		}

		if header["alg"] != "HS256" {
			t.Errorf("expected alg=HS256, got %s", header["alg"])
		}
		if header["typ"] != "JWT" {
			t.Errorf("expected typ=JWT, got %s", header["typ"])
		}
	})

	t.Run("includes issuer (iss) matching the access key", func(t *testing.T) {
		token, err := generateJWTToken("my-access-key-123", "my-secret-key")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		parts := strings.Split(token, ".")
		payloadJSON, err := base64urlDecode(parts[1])
		if err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		if payload["iss"] != "my-access-key-123" {
			t.Errorf("expected iss=my-access-key-123, got %v", payload["iss"])
		}
	})

	t.Run("includes exp and nbf claims", func(t *testing.T) {
		token, err := generateJWTToken("test-ak", "test-sk")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		parts := strings.Split(token, ".")
		payloadJSON, err := base64urlDecode(parts[1])
		if err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		exp, ok := payload["exp"].(float64)
		if !ok {
			t.Fatal("exp claim not found or not a number")
		}

		nbf, ok := payload["nbf"].(float64)
		if !ok {
			t.Fatal("nbf claim not found or not a number")
		}

		// exp should be approximately 30 minutes from nbf
		diff := int64(exp) - int64(nbf)
		if diff < 1800-10 || diff > 1800+10 {
			t.Errorf("expected exp-nbf to be ~1800, got %d", diff)
		}
	})

	t.Run("exp is in the future", func(t *testing.T) {
		token, err := generateJWTToken("test-ak", "test-sk")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		parts := strings.Split(token, ".")
		payloadJSON, err := base64urlDecode(parts[1])
		if err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		exp, ok := payload["exp"].(float64)
		if !ok {
			t.Fatal("exp claim not found or not a number")
		}

		now := time.Now().Unix()
		if int64(exp) <= now {
			t.Error("exp should be in the future")
		}
	})

	t.Run("produces different tokens for different secret keys", func(t *testing.T) {
		token1, err := generateJWTToken("same-ak", "secret-key-1")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		token2, err := generateJWTToken("same-ak", "secret-key-2")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Signatures should differ
		sig1 := strings.Split(token1, ".")[2]
		sig2 := strings.Split(token2, ".")[2]
		if sig1 == sig2 {
			t.Error("signatures should differ for different secret keys")
		}
	})

	t.Run("returns error when access key is empty", func(t *testing.T) {
		_, err := generateJWTToken("", "test-sk")
		if err == nil {
			t.Error("expected error for empty access key")
		}
		if !strings.Contains(err.Error(), "access key") {
			t.Errorf("expected error about access key, got: %v", err)
		}
	})

	t.Run("returns error when secret key is empty", func(t *testing.T) {
		_, err := generateJWTToken("test-ak", "")
		if err == nil {
			t.Error("expected error for empty secret key")
		}
		if !strings.Contains(err.Error(), "secret key") {
			t.Errorf("expected error about secret key, got: %v", err)
		}
	})
}

func TestBase64urlEncode(t *testing.T) {
	t.Run("encodes data correctly", func(t *testing.T) {
		data := []byte("hello world")
		encoded := base64urlEncode(data)

		// Should not contain +, /, or =
		if strings.Contains(encoded, "+") {
			t.Error("encoded string should not contain +")
		}
		if strings.Contains(encoded, "/") {
			t.Error("encoded string should not contain /")
		}
		if strings.Contains(encoded, "=") {
			t.Error("encoded string should not contain =")
		}
	})

	t.Run("can be decoded back", func(t *testing.T) {
		original := []byte("test data 123")
		encoded := base64urlEncode(original)
		decoded, err := base64urlDecode(encoded)
		if err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		if string(decoded) != string(original) {
			t.Errorf("decoded data doesn't match original: got %s, want %s", decoded, original)
		}
	})
}

// Helper function to decode base64url
func base64urlDecode(encoded string) ([]byte, error) {
	// Add padding if needed
	padding := len(encoded) % 4
	if padding > 0 {
		encoded += strings.Repeat("=", 4-padding)
	}

	// Convert from base64url to base64
	encoded = strings.ReplaceAll(encoded, "-", "+")
	encoded = strings.ReplaceAll(encoded, "_", "/")

	return base64.StdEncoding.DecodeString(encoded)
}
