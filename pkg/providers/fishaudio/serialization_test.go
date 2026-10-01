package fishaudio

import "testing"

func TestFishAudioSerializeAndDeserializeSpeechModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.SpeechModel("speech-1.6")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := model.(*SpeechModel).Serialize()
	if serialized.Provider != "fish-audio.speech" || serialized.ModelID != "speech-1.6" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("deserializeSpeechModel error = %v", err)
	}
	if restored.Provider() != "fish-audio.speech" || restored.ModelID() != "speech-1.6" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestFishAudioSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.TranscriptionModel("asr")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := model.(*TranscriptionModel).Serialize()
	if serialized.Provider != "fish-audio.transcription" || serialized.ModelID != "asr" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "fish-audio.transcription" || restored.ModelID() != "asr" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
