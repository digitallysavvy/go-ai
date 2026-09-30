package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestGoogleSerializeAndDeserializeModel(t *testing.T) {
	t.Parallel()

	p := New(Config{
		APIKey:  "k",
		BaseURL: "https://example.test/v1beta",
		Name:    "google.generative-ai",
		Headers: map[string]string{"X-Trace": "1"},
	})

	modelAny, err := p.LanguageModel(ModelGemini20Flash)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "google.generative-ai" {
		t.Fatalf("provider = %q, want google.generative-ai", serialized.Provider)
	}
	if serialized.ModelID != ModelGemini20Flash {
		t.Fatalf("modelID = %q", serialized.ModelID)
	}
	if serialized.Config == nil {
		t.Fatal("serialized config must not be nil")
	}
	if _, ok := serialized.Config["headers"]; !ok {
		t.Fatalf("expected serializable headers in config: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("API key must be omitted from serializable config: %#v", serialized.Config)
	}

	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel() error = %v", err)
	}
	if restored.Provider() != "google.generative-ai" {
		t.Fatalf("restored provider = %q", restored.Provider())
	}
	if restored.ModelID() != ModelGemini20Flash {
		t.Fatalf("restored model ID = %q", restored.ModelID())
	}

	restoredByRegistry, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("provider.DeserializeModel() error = %v", err)
	}
	if restoredByRegistry.Provider() != "google.generative-ai" {
		t.Fatalf("registry restored provider = %q", restoredByRegistry.Provider())
	}
}

func TestGoogleDeserializeModel_ErrorForEmptyModelID(t *testing.T) {
	t.Parallel()

	_, err := deserializeModel(provider.SerializedModel{
		Provider: "google",
		ModelID:  "",
		Config:   map[string]interface{}{"baseURL": "https://example.test"},
	})
	if err == nil {
		t.Fatal("expected error for empty model ID")
	}
}

func TestGoogleSerializeAndDeserializeEmbeddingModel(t *testing.T) {
	t.Parallel()
	p := New(Config{APIKey: "k", BaseURL: "https://example.test/v1beta"})
	modelAny, err := p.EmbeddingModel("gemini-embedding-001")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := modelAny.(*EmbeddingModel).Serialize()
	if serialized.Provider != "google.generative-ai" || serialized.ModelID != "gemini-embedding-001" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "google.generative-ai" || restored.ModelID() != "gemini-embedding-001" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "gemini-embedding-001" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestGoogleSerializeAndDeserializeImageModel(t *testing.T) {
	t.Parallel()
	p := New(Config{APIKey: "k", BaseURL: "https://example.test/v1beta"})
	modelAny, err := p.ImageModel("gemini-2.5-flash-image")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := modelAny.(*ImageModel).Serialize()
	if serialized.Provider != "google.generative-ai" || serialized.ModelID != "gemini-2.5-flash-image" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "google.generative-ai" || restored.ModelID() != "gemini-2.5-flash-image" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "gemini-2.5-flash-image" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestGoogleSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	t.Parallel()
	p := New(Config{APIKey: "k", BaseURL: "https://example.test/v1beta"})
	modelAny, err := p.TranscriptionModel("gemini-3.5-transcribe")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := modelAny.(*TranscriptionModel).Serialize()
	if serialized.Provider != "google.generative-ai.transcription" || serialized.ModelID != "gemini-3.5-transcribe" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "google.generative-ai.transcription" || restored.ModelID() != "gemini-3.5-transcribe" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != "gemini-3.5-transcribe" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
