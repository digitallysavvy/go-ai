package quiverai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestQuiverAISerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.quiverai.example/v1"})
	modelAny, err := p.LanguageModel("qr-1")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	model := modelAny.(*LanguageModel)

	serialized, err := model.SerializeStrict()
	if err != nil {
		t.Fatalf("SerializeStrict error = %v", err)
	}
	if serialized.Provider != "quiverai.responses" || serialized.ModelID != "qr-1" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("apiKey should not be serialized: %#v", serialized.Config)
	}

	restored, err := deserializeQuiverAIModel(serialized)
	if err != nil {
		t.Fatalf("deserializeQuiverAIModel error = %v", err)
	}
	if restored.Provider() != "quiverai.responses" || restored.ModelID() != "qr-1" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("provider.SerializeModel error = %v", err)
	}
	restoredViaRegistry, err := provider.DeserializeModel(viaRegistry)
	if err != nil {
		t.Fatalf("provider.DeserializeModel error = %v", err)
	}
	if restoredViaRegistry.ModelID() != "qr-1" {
		t.Fatalf("registry restored mismatch: %#v", restoredViaRegistry)
	}
}
