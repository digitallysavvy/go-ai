package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestSerializeResponsesLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.LanguageModel("grok-4")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}

	serialized := model.(*ResponsesLanguageModel).Serialize()
	if serialized.Provider != "xai.responses" {
		t.Fatalf("provider = %q", serialized.Provider)
	}
	if serialized.ModelID != "grok-4" {
		t.Fatalf("modelID = %q", serialized.ModelID)
	}
}

func TestDeserializeModel(t *testing.T) {
	respModel, err := deserializeModel(provider.SerializedModel{
		Provider: "xai.responses",
		ModelID:  "grok-4",
		Config:   map[string]interface{}{"apiKey": "key"},
	})
	if err != nil {
		t.Fatalf("deserialize responses error = %v", err)
	}
	if respModel.Provider() != "xai.responses" || respModel.ModelID() != "grok-4" {
		t.Fatalf("unexpected responses model: provider=%s model=%s", respModel.Provider(), respModel.ModelID())
	}

	// A model serialized with the legacy "xai" (Chat Completions) provider
	// tag now deserializes to the Responses API model (row 1f20dba: Chat
	// Completions was removed; LanguageModel() is Responses-only).
	legacyTaggedModel, err := deserializeModel(provider.SerializedModel{
		Provider: "xai",
		ModelID:  "grok-3",
		Config:   map[string]interface{}{"apiKey": "key"},
	})
	if err != nil {
		t.Fatalf("deserialize legacy-tagged model error = %v", err)
	}
	if legacyTaggedModel.Provider() != "xai.responses" || legacyTaggedModel.ModelID() != "grok-3" {
		t.Fatalf("unexpected legacy-tagged model: provider=%s model=%s", legacyTaggedModel.Provider(), legacyTaggedModel.ModelID())
	}
}

func TestXaiSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	modelAny, err := p.ImageModel("grok-2-image")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := modelAny.(*ImageModel).Serialize()
	if serialized.Provider != "xai.image" || serialized.ModelID != "grok-2-image" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "xai.image" || restored.ModelID() != "grok-2-image" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "grok-2-image" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestXaiSerializeAndDeserializeSpeechModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	modelAny, err := p.SpeechModel("")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := modelAny.(*SpeechModel).Serialize()
	if serialized.Provider != "xai.speech" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("deserializeSpeechModel error = %v", err)
	}
	if restored.Provider() != "xai.speech" {
		t.Fatalf("restored mismatch: provider=%s", restored.Provider())
	}

	viaRegistry, err := provider.DeserializeSpeechModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeSpeechModel error = %v", err)
	}
	if viaRegistry.Provider() != "xai.speech" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestXaiSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	modelAny, err := p.TranscriptionModel("")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := modelAny.(*TranscriptionModel).Serialize()
	if serialized.Provider != "xai.transcription" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "xai.transcription" {
		t.Fatalf("restored mismatch: provider=%s", restored.Provider())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.Provider() != "xai.transcription" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
