package googlevertex

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("google-vertex", deserializeModel)
	provider.RegisterEmbeddingModelDeserializer("google-vertex", deserializeEmbeddingModel)
	provider.RegisterImageModelDeserializer("google-vertex", deserializeImageModel)
	provider.RegisterTranscriptionModelDeserializer("google.vertex.transcription", deserializeTranscriptionModel)
	provider.RegisterSpeechModelDeserializer("google.vertex.speech", deserializeSpeechModel)
	provider.RegisterModelDeserializer("google.vertex.interactions", deserializeInteractionsModel)
}

func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.LanguageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS google-vertex-embedding-model.ts [WORKFLOW_SERIALIZE].
func (m *EmbeddingModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeEmbeddingModel(serialized provider.SerializedModel) (provider.EmbeddingModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.EmbeddingModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS google-vertex-image-model.ts [WORKFLOW_SERIALIZE].
func (m *ImageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.prov.config)}
}

func deserializeImageModel(serialized provider.SerializedModel) (provider.ImageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.ImageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS google-vertex-transcription-model.ts [WORKFLOW_SERIALIZE].
func (m *TranscriptionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

// deserializeTranscriptionModel reconstructs whichever transcription model
// type Provider.TranscriptionModel would build for this model ID: Chirp
// (TranscriptionModel) for everything else, or Gemini
// (GeminiTranscriptionModel, U10) for "gemini*" IDs -- both are tagged
// "google.vertex.transcription", so ModelID-based routing (identical to
// Provider.TranscriptionModel's own dispatch) is what disambiguates them.
func deserializeTranscriptionModel(serialized provider.SerializedModel) (provider.TranscriptionModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.TranscriptionModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS gemini-transcription/google-vertex-gemini-transcription-model.ts
// [WORKFLOW_SERIALIZE].
func (m *GeminiTranscriptionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS google-vertex-cloud-tts-speech-model.ts [WORKFLOW_SERIALIZE].
// Embeds "kind": "cloud-tts" in Config so deserializeSpeechModel can tell
// this type apart from the Gemini TTS model (googleprovider.SpeechModel,
// Kind: "gemini-tts"), which shares the same "google.vertex.speech"
// Provider() tag -- see the Kind doc comment on
// pkg/providers/google.SpeechModelConfig.
func (m *CloudTTSSpeechModel) Serialize() provider.SerializedModel {
	cfg := provider.SerializableConfig(m.prov.config)
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	cfg["kind"] = "cloud-tts"
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: cfg}
}

// deserializeInteractionsModel restores a serialized Vertex Interactions
// model. InteractionsLanguageModel is the exact same google.package type
// Vertex reuses directly (see interactionsConfig/Provider.Interactions in
// provider.go), so Serialize() is already registered there
// (pkg/providers/google/serialization.go); this only supplies the
// "google.vertex.interactions"-tagged deserializer.
func deserializeInteractionsModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if agent, _ := serialized.Config["agent"].(string); agent != "" {
		return p.InteractionsAgent(agent)
	}
	return p.Interactions(serialized.ModelID)
}

// deserializeSpeechModel reconstructs whichever speech model type
// Provider.SpeechModel would build for this model ID: CloudTTSSpeechModel
// (Chirp 3: HD) when Config["kind"] says "cloud-tts", or the shared Gemini
// TTS googleprovider.SpeechModel otherwise -- matching Provider.SpeechModel's
// own "chirp" prefix dispatch as a fallback for a hand-built SerializedModel
// that omits "kind".
func deserializeSpeechModel(serialized provider.SerializedModel) (provider.SpeechModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if kind, _ := serialized.Config["kind"].(string); kind == "cloud-tts" {
		return NewCloudTTSSpeechModel(p, serialized.ModelID), nil
	}
	return p.SpeechModel(serialized.ModelID)
}
