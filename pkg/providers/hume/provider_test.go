package hume

import "testing"

func TestProviderSupportsSpeech(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})

	if p.Name() != "hume" {
		t.Fatalf("Name() = %q", p.Name())
	}

	speech, err := p.SpeechModel("")
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	if speech.ModelID() != "" || speech.Provider() != "hume.speech" || speech.SpecificationVersion() != "v4" {
		t.Fatalf("speech metadata mismatch: %#v", speech)
	}
}

// TestProviderFallsBackToEnvAPIKey mirrors the TypeScript SDK's loadApiKey
// fallback to the HUME_API_KEY environment variable when no explicit apiKey
// is configured.
func TestProviderFallsBackToEnvAPIKey(t *testing.T) {
	t.Setenv("HUME_API_KEY", "env-api-key")

	p := New(Config{})
	if p.config.APIKey != "env-api-key" {
		t.Fatalf("config.APIKey = %q, want env-api-key", p.config.APIKey)
	}

	p = New(Config{APIKey: "explicit-key"})
	if p.config.APIKey != "explicit-key" {
		t.Fatalf("config.APIKey = %q, want explicit-key", p.config.APIKey)
	}
}

func TestProviderUnsupportedModelTypes(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})

	if _, err := p.LanguageModel("m"); err == nil {
		t.Fatal("expected LanguageModel error")
	}
	if _, err := p.EmbeddingModel("m"); err == nil {
		t.Fatal("expected EmbeddingModel error")
	}
	if _, err := p.ImageModel("m"); err == nil {
		t.Fatal("expected ImageModel error")
	}
	if _, err := p.TranscriptionModel("m"); err == nil {
		t.Fatal("expected TranscriptionModel error")
	}
	if _, err := p.RerankingModel("m"); err == nil {
		t.Fatal("expected RerankingModel error")
	}
}
