package moonshot

import "testing"

func TestMoonshotProviderUnsupportedModelsAndDefaults(t *testing.T) {
	p := New(Config{APIKey: "k"})
	modelAny, err := p.LanguageModel("")
	if err != nil {
		t.Fatalf("LanguageModel(default) error = %v", err)
	}
	if modelAny.ModelID() != "moonshot-v1-32k" {
		t.Fatalf("default model id = %q", modelAny.ModelID())
	}
	if modelAny.SupportsImageInput() {
		t.Fatal("SupportsImageInput() should be false for current Moonshot models")
	}

	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("EmbeddingModel should return unsupported error")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("ImageModel should return unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel should return unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel should return unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel should return unsupported error")
	}
}

func TestMoonshotNewConfigDefaults(t *testing.T) {
	cfg, err := NewConfig("k")
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	if cfg.APIKey != "k" {
		t.Fatalf("NewConfig APIKey = %q", cfg.APIKey)
	}
	if cfg.BaseURL != "" {
		t.Fatalf("NewConfig BaseURL = %q, want empty (default is applied by provider constructor)", cfg.BaseURL)
	}
}
