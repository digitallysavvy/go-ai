package topaz

import "testing"

// TestSerializeAndDeserializeImageModel mirrors TS "supports workflow
// serialization" for TopazImageModel.
func TestSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.ImageModel(ImageModelWonder35)
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := model.(*ImageModel).Serialize()
	if serialized.Provider != "topaz.image" || serialized.ModelID != ImageModelWonder35 || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "topaz.image" || restored.ModelID() != ImageModelWonder35 {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

// TestSerializeAndDeserializeVideoModel mirrors TS "supports workflow
// serialization" for TopazVideoModel.
func TestSerializeAndDeserializeVideoModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.VideoModel(VideoModelStarlightPrecise26)
	if err != nil {
		t.Fatalf("VideoModel error = %v", err)
	}
	serialized := model.(*VideoModel).Serialize()
	if serialized.Provider != "topaz.video" || serialized.ModelID != VideoModelStarlightPrecise26 {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeVideoModel(serialized)
	if err != nil {
		t.Fatalf("deserializeVideoModel error = %v", err)
	}
	if restored.Provider() != "topaz.video" || restored.ModelID() != VideoModelStarlightPrecise26 {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
