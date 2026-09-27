package luma

import (
	"testing"
)

// TestNewProvider_Defaults mirrors TS "should construct an image model with default configuration".
func TestNewProvider_Defaults(t *testing.T) {
	t.Setenv("LUMA_API_KEY", "")
	prov := New(Config{APIKey: "test-key"})
	if prov.Name() != "luma" {
		t.Errorf("Name() = %q, want luma", prov.Name())
	}
	if prov.baseURL() != defaultBaseURL {
		t.Errorf("baseURL() = %q, want %q", prov.baseURL(), defaultBaseURL)
	}
}

// TestNewProvider_APIKeyFromEnv verifies the LUMA_API_KEY environment
// variable is used when Config.APIKey is empty.
func TestNewProvider_APIKeyFromEnv(t *testing.T) {
	t.Setenv("LUMA_API_KEY", "env-key")
	prov := New(Config{})
	if prov.config.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key", prov.config.APIKey)
	}
}

// TestNewProvider_CustomBaseURLAndHeaders mirrors TS "should respect custom configuration options".
func TestNewProvider_CustomBaseURLAndHeaders(t *testing.T) {
	prov := New(Config{
		APIKey:  "custom-api-key",
		BaseURL: "https://custom-api.lumalabs.ai",
		Headers: map[string]string{"X-Custom-Header": "value"},
	})
	if prov.baseURL() != "https://custom-api.lumalabs.ai" {
		t.Errorf("baseURL() = %q", prov.baseURL())
	}
}

func TestProvider_ImageModel(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	m, err := prov.ImageModel("photon-flash-1")
	if err != nil {
		t.Fatalf("ImageModel() error: %v", err)
	}
	if m.ModelID() != "photon-flash-1" {
		t.Errorf("ModelID() = %q", m.ModelID())
	}

	// Empty model ID defaults to photon-1 (TS provider.image default has no
	// implicit default id, but Go's factory convention across providers
	// defaults an empty id rather than erroring).
	def, err := prov.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel(\"\") error: %v", err)
	}
	if def.ModelID() != ModelPhoton1 {
		t.Errorf("default ModelID() = %q, want %q", def.ModelID(), ModelPhoton1)
	}

	if alias, err := prov.Image("photon-1"); err != nil || alias.ModelID() != "photon-1" {
		t.Errorf("Image() alias = %v, %v", alias, err)
	}
}

func TestProvider_UnsupportedModels(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	if _, err := prov.LanguageModel("x"); err == nil {
		t.Error("LanguageModel should be unsupported")
	}
	if _, err := prov.EmbeddingModel("x"); err == nil {
		t.Error("EmbeddingModel should be unsupported")
	}
	if _, err := prov.SpeechModel("x"); err == nil {
		t.Error("SpeechModel should be unsupported")
	}
	if _, err := prov.TranscriptionModel("x"); err == nil {
		t.Error("TranscriptionModel should be unsupported")
	}
	if _, err := prov.RerankingModel("x"); err == nil {
		t.Error("RerankingModel should be unsupported")
	}
}

func TestCreateLuma(t *testing.T) {
	prov := CreateLuma(Config{APIKey: "test-key"})
	if prov == nil || prov.Name() != "luma" {
		t.Fatalf("CreateLuma() = %v", prov)
	}
}
