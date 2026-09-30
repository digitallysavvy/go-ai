package cohere

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestCohereSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.cohere.ai/v1"})
	modelAny, err := p.LanguageModel("command-r-plus")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "cohere" || serialized.ModelID != "command-r-plus" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "cohere" || restored.ModelID() != "command-r-plus" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestCohereSerializeAndDeserializeEmbeddingModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.cohere.ai/v1"})
	modelAny, err := p.EmbeddingModel("embed-english-v3.0")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := modelAny.(*EmbeddingModel).Serialize()
	if serialized.Provider != "cohere" || serialized.ModelID != "embed-english-v3.0" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "cohere" || restored.ModelID() != "embed-english-v3.0" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{
		Provider: serialized.Provider,
		ModelID:  serialized.ModelID,
		Config:   serialized.Config,
	})
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "embed-english-v3.0" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
