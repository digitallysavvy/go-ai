package together

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestTogetherSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.together.xyz/v1"})
	modelAny, err := p.ImageModel("black-forest-labs/FLUX.1-schnell")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := modelAny.(*ImageModel).Serialize()
	if serialized.Provider != "together" || serialized.ModelID != "black-forest-labs/FLUX.1-schnell" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "together" || restored.ModelID() != "black-forest-labs/FLUX.1-schnell" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "black-forest-labs/FLUX.1-schnell" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
