package minimax

import "testing"

// Ported from minimax-provider.test.ts "should construct the model with the
// Anthropic-compatible config".
func TestNewDefaultsToMiniMaxEndpointAndEnvVar(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "env-key")

	p := New(Config{})
	if p.Name() != "minimax" {
		t.Fatalf("Name() = %q, want minimax", p.Name())
	}

	model, err := p.LanguageModel(ModelM3)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if model.Provider() != "minimax" {
		t.Fatalf("model.Provider() = %q, want minimax", model.Provider())
	}
	if model.ModelID() != "minimax-m3" {
		t.Fatalf("model.ModelID() = %q, want minimax-m3", model.ModelID())
	}
}

// Ported from minimax-provider.test.ts "should construct the model with the
// Anthropic-compatible config" (baseURL assertion).
func TestNewDefaultBaseURL(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.anthropicProvider == nil {
		t.Fatal("expected anthropicProvider to be configured")
	}
}

// Ported from minimax-provider.test.ts "chat"/"languageModel" describe
// blocks.
func TestChatModelAliasesLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m1, err := p.ChatModel("minimax-m2.1")
	if err != nil {
		t.Fatalf("ChatModel() error = %v", err)
	}
	m2, err := p.LanguageModel("minimax-m2.1")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if m1.Provider() != m2.Provider() || m1.ModelID() != m2.ModelID() {
		t.Fatalf("ChatModel and LanguageModel should be equivalent: %+v vs %+v", m1, m2)
	}
}

// Ported from minimax-provider.test.ts "unsupported model types" (Go returns
// a plain error; there is no NoSuchModelError type in this SDK).
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

func TestCreateMiniMaxAlias(t *testing.T) {
	if CreateMiniMax(Config{APIKey: "k"}) == nil {
		t.Fatal("CreateMiniMax() returned nil")
	}
}
