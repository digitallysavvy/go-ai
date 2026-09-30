package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestMistralSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.mistral.ai"})
	modelAny, err := p.LanguageModel("mistral-small-latest")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "mistral" || serialized.ModelID != "mistral-small-latest" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "mistral" || restored.ModelID() != "mistral-small-latest" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestMistralSerializeAndDeserializeEmbeddingModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.mistral.ai"})
	modelAny, err := p.EmbeddingModel("mistral-embed")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := modelAny.(*EmbeddingModel).Serialize()
	if serialized.Provider != "mistral" || serialized.ModelID != "mistral-embed" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "mistral" || restored.ModelID() != "mistral-embed" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "mistral-embed" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestMistralSerializeAndDeserializeSpeechModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.mistral.ai"})
	modelAny, err := p.SpeechModel("voxtral-tts")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := modelAny.(*SpeechModel).Serialize()
	if serialized.Provider != "mistral" || serialized.ModelID != "voxtral-tts" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("deserializeSpeechModel error = %v", err)
	}
	if restored.Provider() != "mistral" || restored.ModelID() != "voxtral-tts" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeSpeechModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeSpeechModel error = %v", err)
	}
	if viaRegistry.ModelID() != "voxtral-tts" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestMistralSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.mistral.ai"})
	modelAny, err := p.TranscriptionModel("voxtral-mini-latest")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := modelAny.(*TranscriptionModel).Serialize()
	if serialized.Provider != "mistral" || serialized.ModelID != "voxtral-mini-latest" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "mistral" || restored.ModelID() != "voxtral-mini-latest" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != "voxtral-mini-latest" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
