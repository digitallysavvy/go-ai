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

// TestResolveKlingAIAuthToken ports klingai-auth.test.ts's
// "resolveKlingAIAuthToken" describe block, covering the single-API-key /
// legacy-pair precedence added in TS 29a7a58.
func TestResolveKlingAIAuthToken(t *testing.T) {
	t.Run("returns the api key verbatim when passed explicitly", func(t *testing.T) {
		token, err := resolveKlingAIAuthToken("test-api-key", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "test-api-key" {
			t.Errorf("token = %q, want %q", token, "test-api-key")
		}
	})

	t.Run("trims the api key", func(t *testing.T) {
		token, err := resolveKlingAIAuthToken("  test-api-key  ", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "test-api-key" {
			t.Errorf("token = %q, want %q", token, "test-api-key")
		}
	})

	t.Run("loads the api key from the environment", func(t *testing.T) {
		t.Setenv("KLINGAI_API_KEY", "env-api-key")
		token, err := resolveKlingAIAuthToken("", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "env-api-key" {
			t.Errorf("token = %q, want %q", token, "env-api-key")
		}
	})

	t.Run("prefers an explicit api key over the environment variable", func(t *testing.T) {
		t.Setenv("KLINGAI_API_KEY", "env-api-key")
		token, err := resolveKlingAIAuthToken("explicit-api-key", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "explicit-api-key" {
			t.Errorf("token = %q, want %q", token, "explicit-api-key")
		}
	})

	t.Run("prefers an explicit api key over explicit legacy credentials", func(t *testing.T) {
		token, err := resolveKlingAIAuthToken("explicit-api-key", "test-ak", "test-sk")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "explicit-api-key" {
			t.Errorf("token = %q, want %q", token, "explicit-api-key")
		}
	})

	t.Run("prefers explicit legacy credentials over the api key environment variable", func(t *testing.T) {
		t.Setenv("KLINGAI_API_KEY", "env-api-key")
		token, err := resolveKlingAIAuthToken("", "test-ak", "test-sk")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			t.Fatalf("expected a JWT, got %q", token)
		}
		payloadJSON, err := base64urlDecode(parts[1])
		if err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if payload["iss"] != "test-ak" {
			t.Errorf("iss = %v, want %q", payload["iss"], "test-ak")
		}
	})

	t.Run("prefers the api key environment variable over legacy environment variables", func(t *testing.T) {
		t.Setenv("KLINGAI_API_KEY", "env-api-key")
		t.Setenv("KLINGAI_ACCESS_KEY", "env-access-key")
		t.Setenv("KLINGAI_SECRET_KEY", "env-secret-key")
		token, err := resolveKlingAIAuthToken("", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "env-api-key" {
			t.Errorf("token = %q, want %q", token, "env-api-key")
		}
	})

	t.Run("falls back to a signed JWT when only legacy environment variables are set", func(t *testing.T) {
		t.Setenv("KLINGAI_ACCESS_KEY", "env-access-key")
		t.Setenv("KLINGAI_SECRET_KEY", "env-secret-key")
		token, err := resolveKlingAIAuthToken("", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			t.Fatalf("expected a JWT, got %q", token)
		}
		payloadJSON, err := base64urlDecode(parts[1])
		if err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if payload["iss"] != "env-access-key" {
			t.Errorf("iss = %v, want %q", payload["iss"], "env-access-key")
		}
	})

	t.Run("ignores a blank api key and falls back to legacy credentials", func(t *testing.T) {
		t.Setenv("KLINGAI_API_KEY", "   ")
		t.Setenv("KLINGAI_ACCESS_KEY", "env-access-key")
		t.Setenv("KLINGAI_SECRET_KEY", "env-secret-key")
		token, err := resolveKlingAIAuthToken("", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
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
		if payload["iss"] != "env-access-key" {
			t.Errorf("iss = %v, want %q", payload["iss"], "env-access-key")
		}
	})

	t.Run("throws an api-key-centric error when no credentials are available", func(t *testing.T) {
		_, err := resolveKlingAIAuthToken("", "", "")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "KlingAI API key is missing") ||
			!strings.Contains(err.Error(), "'apiKey'") ||
			!strings.Contains(err.Error(), "KLINGAI_API_KEY") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("still reports the missing secret key when only an access key is set", func(t *testing.T) {
		t.Setenv("KLINGAI_ACCESS_KEY", "env-access-key")
		_, err := resolveKlingAIAuthToken("", "", "")
		if err == nil || !strings.Contains(err.Error(), "KlingAI secret key") {
			t.Errorf("unexpected error: %v", err)
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
