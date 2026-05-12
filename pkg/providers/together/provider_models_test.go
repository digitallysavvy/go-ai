package together

import (
	"testing"
)

func TestProviderModelFactoriesAndUnsupportedMethods(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "together" {
		t.Fatalf("Name() = %q", p.Name())
	}

	lmAny, err := p.LanguageModel("")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	lm := lmAny.(*LanguageModel)
	if lm.ModelID() != "mistralai/Mixtral-8x7B-Instruct-v0.1" {
		t.Fatalf("default language model id mismatch: %q", lm.ModelID())
	}

	emAny, err := p.EmbeddingModel("")
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	em := emAny.(*EmbeddingModel)
	if em.ModelID() != "togethercomputer/m2-bert-80M-8k-retrieval" {
		t.Fatalf("default embedding model id mismatch: %q", em.ModelID())
	}

	imAny, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	im := imAny.(*ImageModel)
	if im.ModelID() != "stabilityai/stable-diffusion-xl-base-1.0" {
		t.Fatalf("default image model id mismatch: %q", im.ModelID())
	}

	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("expected speech unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("expected transcription unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected reranking unsupported error")
	}
}

func TestCreateTogetherAIAndSerializationRoundTrip(t *testing.T) {
	p := CreateTogetherAI(Config{
		APIKey:  "k",
		BaseURL: "http://localhost:1234",
		Headers: map[string]string{"X-Custom": "1"},
	})
	if p == nil || p.Client() == nil {
		t.Fatal("expected provider and client")
	}

	modelAny, err := p.LanguageModel("meta-llama/Llama-3-8b")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	model := modelAny.(*LanguageModel)
	serialized := model.Serialize()
	if serialized.Provider != "together" || serialized.ModelID != "meta-llama/Llama-3-8b" {
		t.Fatalf("unexpected serialized model: %+v", serialized)
	}

	restoredAny, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel() error = %v", err)
	}
	restored := restoredAny.(*LanguageModel)
	if restored.ModelID() != "meta-llama/Llama-3-8b" {
		t.Fatalf("restored model id mismatch: %q", restored.ModelID())
	}
}
