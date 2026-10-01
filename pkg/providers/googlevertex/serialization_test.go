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

	_, err = deserializeModel(serialized)
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

	restored, err := deserializeModel(manual)
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
	if _, ok := serialized.Config["AccessToken"]; ok {
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
	if _, ok := serialized.Config["AccessToken"]; ok {
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
	// Chirp model ID: gemini* IDs route to GeminiTranscriptionModel (U10),
	// whose serialization is tracked separately (SER2).
	t.Parallel()
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.TranscriptionModel("chirp_3")
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}
	serialized := modelAny.(*TranscriptionModel).Serialize()
	if serialized.Provider != "google.vertex.transcription" || serialized.ModelID != "chirp_3" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["AccessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	manual := provider.SerializedModel{
		Provider: "google.vertex.transcription",
		ModelID:  "chirp_3",
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
	if restored.Provider() != "google.vertex.transcription" || restored.ModelID() != "chirp_3" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != "chirp_3" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

// TestVertexSerializeAndDeserializeGeminiTranscriptionModel covers the U10
// GeminiTranscriptionModel, which shares the "google.vertex.transcription"
// Provider() tag with TranscriptionModel (Chirp) above; the deserializer
// (Provider.TranscriptionModel's own "gemini*" prefix dispatch) is what
// tells them apart.
func TestVertexSerializeAndDeserializeGeminiTranscriptionModel(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.TranscriptionModel(ModelGemini35Transcribe)
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}
	if _, ok := modelAny.(*GeminiTranscriptionModel); !ok {
		t.Fatalf("expected *GeminiTranscriptionModel, got %T", modelAny)
	}
	serialized := modelAny.(*GeminiTranscriptionModel).Serialize()
	if serialized.Provider != "google.vertex.transcription" || serialized.ModelID != ModelGemini35Transcribe {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["AccessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	// The credential is stripped by design (see the transcription test
	// above), so deserializing the auto-serialized config fails; a manual
	// config with accessToken restored proves the rest round-trips.
	if _, err := deserializeTranscriptionModel(serialized); err == nil {
		t.Fatal("expected deserializeTranscriptionModel() to fail because serialized config omits auth tokens")
	}

	manual := provider.SerializedModel{
		Provider: "google.vertex.transcription",
		ModelID:  ModelGemini35Transcribe,
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
	if _, ok := restored.(*GeminiTranscriptionModel); !ok {
		t.Fatalf("expected restored *GeminiTranscriptionModel, got %T", restored)
	}
	if restored.Provider() != "google.vertex.transcription" || restored.ModelID() != ModelGemini35Transcribe {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != ModelGemini35Transcribe {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

// TestVertexSerializeAndDeserializeSpeechModel_GeminiTTS covers the shared
// Gemini TTS SpeechModel (SER2), which is tagged "google.vertex.speech" --
// the same Provider() tag CloudTTSSpeechModel uses for Chirp. Config["kind"]
// is what lets deserializeSpeechModel rebuild the right Go type.
func TestVertexSerializeAndDeserializeSpeechModel_GeminiTTS(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.SpeechModel(SpeechModelGemini25FlashTTS)
	if err != nil {
		t.Fatalf("SpeechModel() error = %v", err)
	}
	serializable, ok := modelAny.(provider.SerializableModel)
	if !ok {
		t.Fatalf("expected SpeechModel to implement provider.SerializableModel, got %T", modelAny)
	}
	serialized := serializable.Serialize()
	if serialized.Provider != "google.vertex.speech" || serialized.ModelID != SpeechModelGemini25FlashTTS {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if kind, _ := serialized.Config["kind"].(string); kind != "gemini-tts" {
		t.Fatalf("expected kind=gemini-tts: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["AccessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	if _, err := deserializeSpeechModel(serialized); err == nil {
		t.Fatal("expected deserializeSpeechModel() to fail because serialized config omits auth tokens")
	}

	manual := provider.SerializedModel{
		Provider: "google.vertex.speech",
		ModelID:  SpeechModelGemini25FlashTTS,
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
			"kind":        "gemini-tts",
		},
	}
	restored, err := deserializeSpeechModel(manual)
	if err != nil {
		t.Fatalf("deserializeSpeechModel(manual) error = %v", err)
	}
	if _, ok := restored.(*CloudTTSSpeechModel); ok {
		t.Fatalf("expected Gemini TTS restore, got CloudTTSSpeechModel")
	}
	if restored.Provider() != "google.vertex.speech" || restored.ModelID() != SpeechModelGemini25FlashTTS {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeSpeechModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeSpeechModel error = %v", err)
	}
	if viaRegistry.ModelID() != SpeechModelGemini25FlashTTS {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

// TestVertexSerializeAndDeserializeSpeechModel_CloudTTS covers Chirp 3: HD
// (CloudTTSSpeechModel), the other half of the "google.vertex.speech" tag.
func TestVertexSerializeAndDeserializeSpeechModel_CloudTTS(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.SpeechModel("chirp-3-hd")
	if err != nil {
		t.Fatalf("SpeechModel() error = %v", err)
	}
	model, ok := modelAny.(*CloudTTSSpeechModel)
	if !ok {
		t.Fatalf("expected *CloudTTSSpeechModel, got %T", modelAny)
	}
	serialized := model.Serialize()
	if serialized.Provider != "google.vertex.speech" || serialized.ModelID != "chirp-3-hd" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if kind, _ := serialized.Config["kind"].(string); kind != "cloud-tts" {
		t.Fatalf("expected kind=cloud-tts: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["AccessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	if _, err := deserializeSpeechModel(serialized); err == nil {
		t.Fatal("expected deserializeSpeechModel() to fail because serialized config omits auth tokens")
	}

	manual := provider.SerializedModel{
		Provider: "google.vertex.speech",
		ModelID:  "chirp-3-hd",
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
			"kind":        "cloud-tts",
		},
	}
	restored, err := deserializeSpeechModel(manual)
	if err != nil {
		t.Fatalf("deserializeSpeechModel(manual) error = %v", err)
	}
	if _, ok := restored.(*CloudTTSSpeechModel); !ok {
		t.Fatalf("expected restored *CloudTTSSpeechModel, got %T", restored)
	}
	if restored.Provider() != "google.vertex.speech" || restored.ModelID() != "chirp-3-hd" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeSpeechModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeSpeechModel error = %v", err)
	}
	if viaRegistry.ModelID() != "chirp-3-hd" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

// TestVertexDeserializeSpeechModel_NoKindFallsBackToModelIDRouting covers a
// serialized payload predating the "kind" discriminator (SER2): with
// Config["kind"] absent entirely, deserializeSpeechModel must still route
// correctly, using the same "chirp" ModelID prefix check
// Provider.SpeechModel itself uses, rather than erroring or guessing wrong.
func TestVertexDeserializeSpeechModel_NoKindFallsBackToModelIDRouting(t *testing.T) {
	t.Parallel()

	chirp := provider.SerializedModel{
		Provider: "google.vertex.speech",
		ModelID:  "chirp-3-hd",
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
		},
	}
	restored, err := deserializeSpeechModel(chirp)
	if err != nil {
		t.Fatalf("deserializeSpeechModel(no kind, chirp ModelID) error = %v", err)
	}
	if _, ok := restored.(*CloudTTSSpeechModel); !ok {
		t.Fatalf("expected *CloudTTSSpeechModel for a chirp-prefixed ModelID with no kind, got %T", restored)
	}

	gemini := provider.SerializedModel{
		Provider: "google.vertex.speech",
		ModelID:  SpeechModelGemini25FlashTTS,
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
		},
	}
	restoredGemini, err := deserializeSpeechModel(gemini)
	if err != nil {
		t.Fatalf("deserializeSpeechModel(no kind, gemini ModelID) error = %v", err)
	}
	if _, ok := restoredGemini.(*CloudTTSSpeechModel); ok {
		t.Fatalf("expected Gemini TTS (not CloudTTSSpeechModel) for a non-chirp ModelID with no kind")
	}
	if restoredGemini.ModelID() != SpeechModelGemini25FlashTTS {
		t.Fatalf("restored mismatch: %#v", restoredGemini)
	}
}

// TestVertexSerializeAndDeserializeInteractionsModel covers
// InteractionsLanguageModel (shared with pkg/providers/google, see
// interactions_model.go's SerializableConfig doc comment) tagged
// "google.vertex.interactions".
func TestVertexSerializeAndDeserializeInteractionsModel(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.Interactions("gemini-2.5-pro")
	if err != nil {
		t.Fatalf("Interactions() error = %v", err)
	}
	serializable, ok := modelAny.(provider.SerializableModel)
	if !ok {
		t.Fatalf("expected SerializableModel, got %T", modelAny)
	}
	serialized := serializable.Serialize()
	if serialized.Provider != "google.vertex.interactions" || serialized.ModelID != "gemini-2.5-pro" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["AccessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	if _, err := deserializeInteractionsModel(serialized); err == nil {
		t.Fatal("expected deserializeInteractionsModel() to fail because serialized config omits auth tokens")
	}

	manual := provider.SerializedModel{
		Provider: "google.vertex.interactions",
		ModelID:  "gemini-2.5-pro",
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
		},
	}
	restored, err := deserializeInteractionsModel(manual)
	if err != nil {
		t.Fatalf("deserializeInteractionsModel(manual) error = %v", err)
	}
	if restored.Provider() != "google.vertex.interactions" || restored.ModelID() != "gemini-2.5-pro" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeModel error = %v", err)
	}
	if viaRegistry.ModelID() != "gemini-2.5-pro" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
