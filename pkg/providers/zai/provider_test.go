package zai

import "testing"

// Ported from zai-provider.test.ts "should construct the model with the
// Anthropic-compatible config" (adapted: Go returns plain errors, not
// NoSuchModelError, for unsupported model types).
func TestNewDefaultsToZaiEndpointAndEnvVar(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "env-key")

	p := New(Config{})
	if p.Name() != "zai" {
		t.Fatalf("Name() = %q, want zai", p.Name())
	}

	model, err := p.LanguageModel("glm-5.3")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if model.Provider() != "zai" {
		t.Fatalf("model.Provider() = %q, want zai", model.Provider())
	}
	if model.ModelID() != "glm-5.3" {
		t.Fatalf("model.ModelID() = %q, want glm-5.3", model.ModelID())
	}
}

func TestNewRespectsCustomBaseURL(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://example.com/zai"})
	if p.client == nil {
		t.Fatal("expected client to be configured")
	}
}

// Ported from zai-provider.test.ts "should construct a chat model" /
// "should construct a language model".
func TestChatModelAliasesLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m1, err := p.ChatModel("glm-5.3")
	if err != nil {
		t.Fatalf("ChatModel() error = %v", err)
	}
	m2, err := p.LanguageModel("glm-5.3")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if m1.Provider() != m2.Provider() || m1.ModelID() != m2.ModelID() {
		t.Fatalf("ChatModel and LanguageModel should be equivalent: %+v vs %+v", m1, m2)
	}
}

// Ported from zai-provider.test.ts "unsupported model types" (Go returns a
// plain error; there is no NoSuchModelError type in this SDK).
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

func TestCreateZaiAlias(t *testing.T) {
	if CreateZai(Config{APIKey: "k"}) == nil {
		t.Fatal("CreateZai() returned nil")
	}
}
