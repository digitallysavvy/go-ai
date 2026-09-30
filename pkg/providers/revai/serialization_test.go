package revai

import "testing"

func TestRevaiSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.TranscriptionModel("machine")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := model.(*TranscriptionModel).Serialize()
	if serialized.Provider != "revai.transcription" || serialized.ModelID != "machine" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "revai.transcription" || restored.ModelID() != "machine" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
