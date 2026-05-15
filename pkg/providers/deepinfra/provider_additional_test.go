package deepinfra

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func TestDeepInfraProviderWrappers(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "deepinfra" {
		t.Fatalf("Name() = %q, want deepinfra", p.Name())
	}
	if p.Provider == nil {
		t.Fatal("embedded OpenAI provider should not be nil")
	}

	model, err := p.LanguageModel("meta-llama")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if model.Provider() != "deepinfra" {
		t.Fatalf("LanguageModel.Provider() = %q", model.Provider())
	}
}

func TestNewLanguageModelWrapper(t *testing.T) {
	base := openai.New(openai.Config{APIKey: "k", BaseURL: "https://example.invalid"})
	m := NewLanguageModel(base, "gpt-test")
	if m == nil || m.LanguageModel == nil {
		t.Fatal("NewLanguageModel() should wrap OpenAI language model")
	}
	if got := m.Provider(); got != "deepinfra" {
		t.Fatalf("Provider() = %q, want deepinfra", got)
	}
}
