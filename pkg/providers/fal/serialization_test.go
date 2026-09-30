package fal

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

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

func TestFalSerializeAndDeserializeSpeechModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.SpeechModel("fal-ai/kokoro/american-english")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := model.(*SpeechModel).Serialize()
	if serialized.Provider != "fal.speech" || serialized.ModelID != "fal-ai/kokoro/american-english" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("deserializeSpeechModel error = %v", err)
	}
	if restored.Provider() != "fal.speech" || restored.ModelID() != "fal-ai/kokoro/american-english" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("provider.DeserializeSpeechModel error = %v", err)
	}
	if viaRegistry.ModelID() != "fal-ai/kokoro/american-english" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestFalSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.TranscriptionModel("wizper")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := model.(*TranscriptionModel).Serialize()
	if serialized.Provider != "fal.transcription" || serialized.ModelID != "wizper" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "fal.transcription" || restored.ModelID() != "wizper" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != "wizper" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
