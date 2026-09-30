package fireworks

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestFireworksSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	modelAny, err := p.LanguageModel("accounts/fireworks/models/llama-v3p1-70b-instruct")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "fireworks" || serialized.ModelID != "accounts/fireworks/models/llama-v3p1-70b-instruct" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "fireworks" || restored.ModelID() != "accounts/fireworks/models/llama-v3p1-70b-instruct" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeModel error = %v", err)
	}
	if viaRegistry.ModelID() != "accounts/fireworks/models/llama-v3p1-70b-instruct" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestFireworksSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	modelAny, err := p.ImageModel("accounts/fireworks/models/flux-1-schnell-fp8")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := modelAny.(*ImageModel).Serialize()
	if serialized.Provider != "fireworks" || serialized.ModelID != "accounts/fireworks/models/flux-1-schnell-fp8" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "fireworks" || restored.ModelID() != "accounts/fireworks/models/flux-1-schnell-fp8" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "accounts/fireworks/models/flux-1-schnell-fp8" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
