package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestGroqSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.groq.com/openai"})
	modelAny, err := p.LanguageModel("llama-3.3-70b-versatile")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "groq" || serialized.ModelID != "llama-3.3-70b-versatile" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "groq" || restored.ModelID() != "llama-3.3-70b-versatile" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestGroqSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	modelAny, err := p.TranscriptionModel("whisper-large-v3")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := modelAny.(*TranscriptionModel).Serialize()
	if serialized.Provider != "groq" || serialized.ModelID != "whisper-large-v3" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "groq" || restored.ModelID() != "whisper-large-v3" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != "whisper-large-v3" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
