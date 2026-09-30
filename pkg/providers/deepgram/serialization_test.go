package deepgram

import "testing"

func TestDeepgramSerializeAndDeserializeSpeechModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.SpeechModel("aura-2-thalia-en")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := model.(*SpeechModel).Serialize()
	if serialized.Provider != "deepgram" || serialized.ModelID != "aura-2-thalia-en" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("deserializeSpeechModel error = %v", err)
	}
	if restored.Provider() != "deepgram" || restored.ModelID() != "aura-2-thalia-en" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestDeepgramSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.TranscriptionModel("nova-3")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := model.(*TranscriptionModel).Serialize()
	if serialized.Provider != "deepgram" || serialized.ModelID != "nova-3" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "deepgram" || restored.ModelID() != "nova-3" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
