package assemblyai

import "testing"

func TestAssemblyAISerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.TranscriptionModel("best")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := model.(*TranscriptionModel).Serialize()
	if serialized.Provider != "assemblyai" || serialized.ModelID != "best" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "assemblyai" || restored.ModelID() != "best" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
