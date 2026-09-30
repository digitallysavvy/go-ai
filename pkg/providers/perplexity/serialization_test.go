package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestPerplexitySerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: DefaultBaseURL})
	modelAny, err := p.LanguageModel(DefaultModelID)
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "perplexity" || serialized.ModelID != DefaultModelID || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "perplexity" || restored.ModelID() != DefaultModelID {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestPerplexitySerializeAndDeserializeEmbeddingModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	modelAny, err := p.EmbeddingModel("perplexity-embed")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := modelAny.(*EmbeddingModel).Serialize()
	if serialized.Provider != "perplexity" || serialized.ModelID != "perplexity-embed" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "perplexity" || restored.ModelID() != "perplexity-embed" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "perplexity-embed" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
