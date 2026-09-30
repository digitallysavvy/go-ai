package gladia

import "testing"

func TestGladiaSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.TranscriptionModel("default")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := model.(*TranscriptionModel).Serialize()
	if serialized.Provider != "gladia" || serialized.ModelID != "default" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "gladia" || restored.ModelID() != "default" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
