package cartesia

import "testing"

func TestCartesiaSerializeAndDeserializeSpeechModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.SpeechModel("sonic-2")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := model.(*SpeechModel).Serialize()
	if serialized.Provider != "cartesia.speech" || serialized.ModelID != "sonic-2" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("deserializeSpeechModel error = %v", err)
	}
	if restored.Provider() != "cartesia.speech" || restored.ModelID() != "sonic-2" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestCartesiaSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.TranscriptionModel("ink-whisper")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := model.(*TranscriptionModel).Serialize()
	if serialized.Provider != "cartesia.transcription" || serialized.ModelID != "ink-whisper" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "cartesia.transcription" || restored.ModelID() != "ink-whisper" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
