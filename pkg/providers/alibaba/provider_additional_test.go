package alibaba

import "testing"

func TestAlibabaProviderCreateAndUnsupportedMethods(t *testing.T) {
	p := CreateAlibaba(Config{APIKey: "k"})
	if p == nil {
		t.Fatal("CreateAlibaba() returned nil")
	}
	if p.Name() != "alibaba" {
		t.Fatalf("Name() = %q, want alibaba", p.Name())
	}
	if p.Client() == nil || p.VideoClient() == nil {
		t.Fatal("Client() and VideoClient() should both be initialized")
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("EmbeddingModel should be unsupported")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("ImageModel should be unsupported")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel should be unsupported")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel should be unsupported")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel should be unsupported")
	}
}

func TestAlibabaConfigDefaults(t *testing.T) {
	cfg, err := NewConfig("api-key")
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	if cfg.APIKey != "api-key" {
		t.Fatalf("NewConfig APIKey = %q", cfg.APIKey)
	}
	if cfg.BaseURL != "" || cfg.VideoBaseURL != "" {
		t.Fatalf("NewConfig should not eagerly set URLs; constructor applies defaults: %#v", cfg)
	}
}
