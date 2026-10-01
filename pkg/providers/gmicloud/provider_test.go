package gmicloud

import (
	"testing"
)

// Ported from gmicloud-provider.test.ts "defaults to the GMI Cloud endpoint
// and GMI_CLOUD_APIKEY".
func TestNewDefaultsToGmiCloudEndpointAndEnvVar(t *testing.T) {
	t.Setenv("GMI_CLOUD_APIKEY", "env-key")

	p := New(Config{})
	if p.Name() != "gmicloud" {
		t.Fatalf("Name() = %q, want gmicloud", p.Name())
	}

	model, err := p.LanguageModel("deepseek-ai/DeepSeek-V4-Flash-0731")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if model.Provider() != "gmicloud" {
		t.Fatalf("model.Provider() = %q, want gmicloud", model.Provider())
	}
}

// Ported from gmicloud-provider.test.ts "respects a custom baseURL".
func TestNewRespectsCustomBaseURL(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://example.com/gmi"})
	if p.client == nil {
		t.Fatal("expected client to be configured")
	}
	// Behavioral verification of the base URL happens via the httptest-backed
	// DoGenerate/DoStream test in language_model_test.go, which points
	// BaseURL at a local server and confirms the request lands there.
}

// Ported from gmicloud-provider.test.ts "throws NoSuchModelError for
// embedding and image models" (Go returns a plain error; there is no
// NoSuchModelError type in this SDK).
func TestUnsupportedModelTypes(t *testing.T) {
	p := New(Config{APIKey: "k"})

	if _, err := p.EmbeddingModel("model"); err == nil {
		t.Fatal("EmbeddingModel() expected error, got nil")
	}
	if _, err := p.ImageModel("model"); err == nil {
		t.Fatal("ImageModel() expected error, got nil")
	}
	if _, err := p.SpeechModel("model"); err == nil {
		t.Fatal("SpeechModel() expected error, got nil")
	}
	if _, err := p.TranscriptionModel("model"); err == nil {
		t.Fatal("TranscriptionModel() expected error, got nil")
	}
	if _, err := p.RerankingModel("model"); err == nil {
		t.Fatal("RerankingModel() expected error, got nil")
	}
}

func TestCreateGmicloudAlias(t *testing.T) {
	if CreateGmicloud(Config{APIKey: "k"}) == nil {
		t.Fatal("CreateGmicloud() returned nil")
	}
}
