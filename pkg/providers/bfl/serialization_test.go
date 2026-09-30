package bfl

import "testing"

func TestBFLSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.ImageModel("flux-pro-1.1")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := model.(*ImageModel).Serialize()
	if serialized.Provider != "bfl" || serialized.ModelID != "flux-pro-1.1" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "bfl" || restored.ModelID() != "flux-pro-1.1" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestBFLSerializeAndDeserializeVideoModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.VideoModel("flux-video")
	if err != nil {
		t.Fatalf("VideoModel error = %v", err)
	}
	serialized := model.(*VideoModel).Serialize()
	if serialized.Provider != "bfl.video" || serialized.ModelID != "flux-video" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeVideoModel(serialized)
	if err != nil {
		t.Fatalf("deserializeVideoModel error = %v", err)
	}
	if restored.Provider() != "bfl.video" || restored.ModelID() != "flux-video" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
