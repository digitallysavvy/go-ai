package gateway

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestGatewaySerializeAndDeserializeLanguageModel(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://ai-gateway.vercel.sh/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	modelAny, err := p.LanguageModel("openai/gpt-4.1")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	model := modelAny.(*LanguageModel)

	serialized := model.Serialize()
	if serialized.Provider != "gateway" || serialized.ModelID != "openai/gpt-4.1" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeModel(provider.SerializedModel{
		Provider: "gateway",
		ModelID:  "openai/gpt-4.1-mini",
		Config:   map[string]interface{}{"apiKey": "k"},
	})
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "gateway" || restored.ModelID() != "openai/gpt-4.1-mini" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestGatewaySerializeAndDeserializeEmbeddingModel(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://ai-gateway.vercel.sh/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	modelAny, err := p.EmbeddingModel("openai/text-embedding-3-small")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := modelAny.(*EmbeddingModel).Serialize()
	if serialized.Provider != "gateway" || serialized.ModelID != "openai/text-embedding-3-small" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "gateway" || restored.ModelID() != "openai/text-embedding-3-small" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "openai/text-embedding-3-small" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestGatewaySerializeAndDeserializeImageModel(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://ai-gateway.vercel.sh/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	modelAny, err := p.ImageModel("openai/dall-e-3")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := modelAny.(*ImageModel).Serialize()
	if serialized.Provider != "gateway" || serialized.ModelID != "openai/dall-e-3" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "gateway" || restored.ModelID() != "openai/dall-e-3" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "openai/dall-e-3" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
