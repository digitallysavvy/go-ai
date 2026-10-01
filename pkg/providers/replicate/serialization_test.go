package replicate

import "testing"

func TestReplicateSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.ImageModel("black-forest-labs/flux-schnell")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := model.(*ImageModel).Serialize()
	if serialized.Provider != "replicate" || serialized.ModelID != "black-forest-labs/flux-schnell" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "replicate" || restored.ModelID() != "black-forest-labs/flux-schnell" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
