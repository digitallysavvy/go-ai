package hume

import "testing"

func TestHumeSerializeAndDeserializeSpeechModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.SpeechModel("default")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := model.(*SpeechModel).Serialize()
	// Hume has no selectable model ID; ModelID always returns "".
	if serialized.Provider != "hume.speech" || serialized.ModelID != "" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "hume.speech" || restored.ModelID() != "" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
