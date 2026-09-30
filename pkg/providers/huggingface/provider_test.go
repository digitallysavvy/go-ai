package huggingface

import (
	"strings"
	"testing"
)

// Ported from huggingface-provider.test.ts "should create provider with
// default configuration" / "should create provider with custom settings".
func TestNewProviderDefaults(t *testing.T) {
	p := New(Config{})
	if p.Name() != "huggingface" {
		t.Fatalf("Name = %q, want huggingface", p.Name())
	}
	if p.client == nil {
		t.Fatal("expected client to be initialized")
	}
}

func TestNewProviderCustomSettings(t *testing.T) {
	p := New(Config{
		APIKey:  "custom-key",
		BaseURL: "https://custom.url",
		Headers: map[string]string{"Custom-Header": "test"},
	})
	if p == nil {
		t.Fatal("expected provider")
	}
}

// Ported from huggingface-provider.test.ts "should expose responses method" /
// "should expose languageModel method".
func TestProviderModelCreationMethods(t *testing.T) {
	p := New(Config{APIKey: "hf"})

	lm, err := p.LanguageModel("deepseek-ai/DeepSeek-V3-0324")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	if lm.ModelID() != "deepseek-ai/DeepSeek-V3-0324" {
		t.Fatalf("ModelID = %q", lm.ModelID())
	}
	if lm.Provider() != "huggingface.responses" {
		t.Fatalf("Provider() = %q, want huggingface.responses", lm.Provider())
	}

	rm, err := p.ResponsesModel("deepseek-ai/DeepSeek-V3-0324")
	if err != nil {
		t.Fatalf("ResponsesModel: %v", err)
	}
	if rm.ModelID() != "deepseek-ai/DeepSeek-V3-0324" {
		t.Fatalf("ResponsesModel ModelID = %q", rm.ModelID())
	}
}

// Ported from huggingface-provider.test.ts "should throw for text embedding
// models" / "should throw for image models".
func TestProviderUnsupportedModels(t *testing.T) {
	p := New(Config{APIKey: "hf"})

	if _, err := p.EmbeddingModel("any-model"); err == nil {
		t.Fatal("EmbeddingModel: expected error")
	} else if !strings.Contains(err.Error(), "Hugging Face Responses API does not support text embeddings") {
		t.Fatalf("EmbeddingModel error = %q", err.Error())
	}

	if _, err := p.ImageModel("any-model"); err == nil {
		t.Fatal("ImageModel: expected error")
	} else if !strings.Contains(err.Error(), "Hugging Face Responses API does not support image generation") {
		t.Fatalf("ImageModel error = %q", err.Error())
	}

	if _, err := p.SpeechModel("any-model"); err == nil {
		t.Fatal("SpeechModel: expected error")
	}
	if _, err := p.TranscriptionModel("any-model"); err == nil {
		t.Fatal("TranscriptionModel: expected error")
	}
	if _, err := p.RerankingModel("any-model"); err == nil {
		t.Fatal("RerankingModel: expected error")
	}
}
