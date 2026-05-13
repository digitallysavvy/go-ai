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
