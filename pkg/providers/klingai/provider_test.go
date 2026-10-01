package klingai

import (
	"encoding/json"
	"strings"
	"testing"
)

// clearKlingAICredentialEnv resets all three KlingAI credential environment
// variables for the duration of the test (via t.Setenv), so a test's
// intended credential source is the only one available.
func clearKlingAICredentialEnv(t *testing.T) {
	t.Helper()
	t.Setenv("KLINGAI_API_KEY", "")
	t.Setenv("KLINGAI_ACCESS_KEY", "")
	t.Setenv("KLINGAI_SECRET_KEY", "")
}

func TestNew(t *testing.T) {
	t.Run("creates provider with explicit config", func(t *testing.T) {
		clearKlingAICredentialEnv(t)
		cfg := Config{
			AccessKey: "test-access-key",
			SecretKey: "test-secret-key",
			BaseURL:   "https://test.example.com",
		}

		prov, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if prov.Name() != "klingai" {
			t.Errorf("expected provider name 'klingai', got %s", prov.Name())
		}

		if prov.config.AccessKey != "test-access-key" {
			t.Errorf("expected access key to be set")
		}

		if prov.config.BaseURL != "https://test.example.com" {
			t.Errorf("expected custom base URL, got %s", prov.config.BaseURL)
		}
	})

	t.Run("creates provider with a single API key", func(t *testing.T) {
		clearKlingAICredentialEnv(t)
		cfg := Config{APIKey: "test-api-key"}

		prov, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		token, err := prov.GenerateAuthToken()
		if err != nil {
			t.Fatalf("GenerateAuthToken() error: %v", err)
		}
		if token != "test-api-key" {
			t.Errorf("token = %q, want %q", token, "test-api-key")
		}
	})

	t.Run("uses default base URL when not provided", func(t *testing.T) {
		clearKlingAICredentialEnv(t)
		cfg := Config{
			AccessKey: "test-access-key",
			SecretKey: "test-secret-key",
		}

		prov, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if prov.config.BaseURL != defaultBaseURL {
			t.Errorf("expected default base URL %s, got %s", defaultBaseURL, prov.config.BaseURL)
		}
	})

	t.Run("loads credentials from environment variables", func(t *testing.T) {
		clearKlingAICredentialEnv(t)
		t.Setenv("KLINGAI_ACCESS_KEY", "env-access-key")
		t.Setenv("KLINGAI_SECRET_KEY", "env-secret-key")

		cfg := Config{}
		prov, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		token, err := prov.GenerateAuthToken()
		if err != nil {
			t.Fatalf("GenerateAuthToken() error: %v", err)
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

	t.Run("loads a single API key from the environment", func(t *testing.T) {
		clearKlingAICredentialEnv(t)
		t.Setenv("KLINGAI_API_KEY", "env-api-key")

		prov, err := New(Config{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		token, err := prov.GenerateAuthToken()
		if err != nil {
			t.Fatalf("GenerateAuthToken() error: %v", err)
		}
		if token != "env-api-key" {
			t.Errorf("token = %q, want %q", token, "env-api-key")
		}
	})

	t.Run("explicit config takes precedence over env variables", func(t *testing.T) {
		clearKlingAICredentialEnv(t)
		t.Setenv("KLINGAI_ACCESS_KEY", "env-access-key")
		t.Setenv("KLINGAI_SECRET_KEY", "env-secret-key")

		cfg := Config{
			AccessKey: "explicit-access-key",
			SecretKey: "explicit-secret-key",
		}

		prov, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if prov.config.AccessKey != "explicit-access-key" {
			t.Errorf("expected explicit access key, got %s", prov.config.AccessKey)
		}
	})

	t.Run("returns error when access key is missing", func(t *testing.T) {
		clearKlingAICredentialEnv(t)

		cfg := Config{
			SecretKey: "test-secret-key",
		}

		_, err := New(cfg)
		if err == nil {
			t.Error("expected error when access key is missing")
		}
	})

	t.Run("returns error when secret key is missing", func(t *testing.T) {
		clearKlingAICredentialEnv(t)

		cfg := Config{
			AccessKey: "test-access-key",
		}

		_, err := New(cfg)
		if err == nil {
			t.Error("expected error when secret key is missing")
		}
	})

	t.Run("returns an api-key-centric error when no credentials at all are configured", func(t *testing.T) {
		clearKlingAICredentialEnv(t)

		_, err := New(Config{})
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "'apiKey'") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestProviderMethods(t *testing.T) {
	cfg := Config{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
	}

	prov, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	t.Run("VideoModel returns model for valid ID", func(t *testing.T) {
		model, err := prov.VideoModel("kling-v2.6-t2v")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if model == nil {
			t.Error("expected model, got nil")
		}
	})

	t.Run("VideoModel returns error for empty ID", func(t *testing.T) {
		_, err := prov.VideoModel("")
		if err == nil {
			t.Error("expected error for empty model ID")
		}
	})

	t.Run("VideoModel returns error for invalid suffix", func(t *testing.T) {
		_, err := prov.VideoModel("kling-v2.6-invalid")
		if err == nil {
			t.Error("expected error for invalid model ID suffix")
		}
	})

	t.Run("LanguageModel returns error", func(t *testing.T) {
		_, err := prov.LanguageModel("test")
		if err == nil {
			t.Error("expected error for unsupported model type")
		}
	})

	t.Run("EmbeddingModel returns error", func(t *testing.T) {
		_, err := prov.EmbeddingModel("test")
		if err == nil {
			t.Error("expected error for unsupported model type")
		}
	})

	t.Run("ImageModel returns error", func(t *testing.T) {
		_, err := prov.ImageModel("test")
		if err == nil {
			t.Error("expected error for unsupported model type")
		}
	})

	t.Run("GenerateAuthToken generates valid token", func(t *testing.T) {
		token, err := prov.GenerateAuthToken()
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if token == "" {
			t.Error("expected non-empty token")
		}
	})
}
