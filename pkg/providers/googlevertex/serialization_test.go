package googlevertex

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVertexSerializeAndDeserializeModel(t *testing.T) {
	t.Parallel()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		BaseURL:     "https://example.test",
		Headers:     map[string]string{"X-Trace": "1"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.LanguageModel("gemini-2.0-flash")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "google-vertex" {
		t.Fatalf("provider = %q, want google-vertex", serialized.Provider)
	}
	if serialized.ModelID != "gemini-2.0-flash" {
		t.Fatalf("modelID = %q", serialized.ModelID)
	}
	if serialized.Config == nil {
		t.Fatal("serialized config must not be nil")
	}
	if _, ok := serialized.Config["headers"]; !ok {
		t.Fatalf("expected serializable headers in config: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	restored, err := deserializeModel(serialized)
	if err == nil {
		t.Fatal("expected deserializeModel() to fail because serialized config omits auth tokens")
	}

	manual := provider.SerializedModel{
		Provider: "google-vertex",
		ModelID:  "gemini-2.0-flash",
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
			"baseURL":     "https://example.test",
		},
	}

	restored, err = deserializeModel(manual)
	if err != nil {
		t.Fatalf("deserializeModel(manual) error = %v", err)
	}
	if restored.Provider() != "google-vertex" || restored.ModelID() != "gemini-2.0-flash" {
		t.Fatalf("manual restored mismatch provider=%q model=%q", restored.Provider(), restored.ModelID())
	}

	restoredByRegistry, err := provider.DeserializeModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeModel() error = %v", err)
	}
	if restoredByRegistry.Provider() != "google-vertex" {
		t.Fatalf("registry restored provider = %q", restoredByRegistry.Provider())
	}
}

func TestVertexDeserializeModel_ErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := deserializeModel(provider.SerializedModel{
		Provider: "google-vertex",
		ModelID:  "gemini-2.0-flash",
		Config:   map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error because project/location/token are required")
	}
}

func TestVertexSerializeAndDeserializeEmbeddingModel(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.EmbeddingModel("text-embedding-004")
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	serialized := modelAny.(*EmbeddingModel).Serialize()
	if serialized.Provider != "google-vertex" || serialized.ModelID != "text-embedding-004" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	manual := provider.SerializedModel{
		Provider: "google-vertex",
		ModelID:  "text-embedding-004",
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
		},
	}
	restored, err := deserializeEmbeddingModel(manual)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel(manual) error = %v", err)
	}
	if restored.Provider() != "google-vertex" || restored.ModelID() != "text-embedding-004" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "text-embedding-004" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestVertexSerializeAndDeserializeImageModel(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.ImageModel("gemini-2.5-flash-image")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	serialized := modelAny.(*ImageModel).Serialize()
	if serialized.Provider != "google-vertex" || serialized.ModelID != "gemini-2.5-flash-image" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	manual := provider.SerializedModel{
		Provider: "google-vertex",
		ModelID:  "gemini-2.5-flash-image",
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
		},
	}
	restored, err := deserializeImageModel(manual)
	if err != nil {
		t.Fatalf("deserializeImageModel(manual) error = %v", err)
	}
	if restored.Provider() != "google-vertex" || restored.ModelID() != "gemini-2.5-flash-image" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "gemini-2.5-flash-image" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestVertexSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.TranscriptionModel("gemini-2.5-flash")
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}
	serialized := modelAny.(*TranscriptionModel).Serialize()
	if serialized.Provider != "google.vertex.transcription" || serialized.ModelID != "gemini-2.5-flash" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	manual := provider.SerializedModel{
		Provider: "google.vertex.transcription",
		ModelID:  "gemini-2.5-flash",
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
		},
	}
	restored, err := deserializeTranscriptionModel(manual)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel(manual) error = %v", err)
	}
	if restored.Provider() != "google.vertex.transcription" || restored.ModelID() != "gemini-2.5-flash" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != "gemini-2.5-flash" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
