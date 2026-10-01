package alibaba

import (
	"os"
	"testing"
)

func TestAlibabaProviderCreateAndUnsupportedMethods(t *testing.T) {
	p := CreateAlibaba(Config{APIKey: "k"})
	if p == nil {
		t.Fatal("CreateAlibaba() returned nil")
	}
	if p.Name() != "alibaba" {
		t.Fatalf("Name() = %q, want alibaba", p.Name())
	}
	if p.Client() == nil || p.VideoClient() == nil || p.EmbeddingClient() == nil {
		t.Fatal("Client(), VideoClient(), and EmbeddingClient() should all be initialized")
	}
	if model, err := p.EmbeddingModel("text-embedding-v4"); err != nil || model == nil {
		t.Fatalf("EmbeddingModel should be supported, model=%v err=%v", model, err)
	}
	if model, err := p.Embedding("text-embedding-v4"); err != nil || model.Provider() != "alibaba.embedding" || model.ModelID() != "text-embedding-v4" {
		t.Fatalf("Embedding alias = %v err=%v", model, err)
	}
	emptyModel, err := p.EmbeddingModel("")
	if err != nil {
		t.Fatalf("EmbeddingModel(\"\") should preserve the caller model ID, got error %v", err)
	}
	if emptyModel.ModelID() != "" {
		t.Fatalf("EmbeddingModel(\"\").ModelID() = %q, want empty string", emptyModel.ModelID())
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

func TestAlibabaProviderAPIKeyFallbackToEnvironment(t *testing.T) {
	const envKey = "env-alibaba-key"
	orig := os.Getenv("ALIBABA_API_KEY")
	t.Cleanup(func() {
		if orig == "" {
			_ = os.Unsetenv("ALIBABA_API_KEY")
			return
		}
		_ = os.Setenv("ALIBABA_API_KEY", orig)
	})
	if err := os.Setenv("ALIBABA_API_KEY", envKey); err != nil {
		t.Fatalf("Setenv failed: %v", err)
	}

	p := New(Config{})
	if p.config.APIKey != envKey {
		t.Fatalf("APIKey = %q, want %q", p.config.APIKey, envKey)
	}
}
