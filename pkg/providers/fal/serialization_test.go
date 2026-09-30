package fal

import "testing"

func TestFalSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.ImageModel("fal-ai/flux/dev")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := model.(*ImageModel).Serialize()
	if serialized.Provider != "fal" || serialized.ModelID != "fal-ai/flux/dev" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "fal" || restored.ModelID() != "fal-ai/flux/dev" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
