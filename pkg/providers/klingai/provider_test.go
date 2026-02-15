package klingai

import (
	"os"
	"testing"
)

func TestNew(t *testing.T) {
	t.Run("creates provider with explicit config", func(t *testing.T) {
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

	t.Run("uses default base URL when not provided", func(t *testing.T) {
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
		os.Setenv("KLINGAI_ACCESS_KEY", "env-access-key")
		os.Setenv("KLINGAI_SECRET_KEY", "env-secret-key")
		defer func() {
			os.Unsetenv("KLINGAI_ACCESS_KEY")
			os.Unsetenv("KLINGAI_SECRET_KEY")
		}()

		cfg := Config{}
		prov, err := New(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if prov.config.AccessKey != "env-access-key" {
			t.Errorf("expected access key from env, got %s", prov.config.AccessKey)
		}

		if prov.config.SecretKey != "env-secret-key" {
			t.Errorf("expected secret key from env, got %s", prov.config.SecretKey)
		}
	})

	t.Run("explicit config takes precedence over env variables", func(t *testing.T) {
		os.Setenv("KLINGAI_ACCESS_KEY", "env-access-key")
		os.Setenv("KLINGAI_SECRET_KEY", "env-secret-key")
		defer func() {
			os.Unsetenv("KLINGAI_ACCESS_KEY")
			os.Unsetenv("KLINGAI_SECRET_KEY")
		}()

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
		os.Unsetenv("KLINGAI_ACCESS_KEY")
		os.Unsetenv("KLINGAI_SECRET_KEY")

		cfg := Config{
			SecretKey: "test-secret-key",
		}

		_, err := New(cfg)
		if err == nil {
			t.Error("expected error when access key is missing")
		}
	})

	t.Run("returns error when secret key is missing", func(t *testing.T) {
		os.Unsetenv("KLINGAI_ACCESS_KEY")
		os.Unsetenv("KLINGAI_SECRET_KEY")

		cfg := Config{
			AccessKey: "test-access-key",
		}

		_, err := New(cfg)
		if err == nil {
			t.Error("expected error when secret key is missing")
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
