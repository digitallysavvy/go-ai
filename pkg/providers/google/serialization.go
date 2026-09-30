package google

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("google", deserializeModel)
	provider.RegisterModelDeserializer("google.generative-ai", deserializeModel)
	provider.RegisterEmbeddingModelDeserializer("google", deserializeEmbeddingModel)
	provider.RegisterEmbeddingModelDeserializer("google.generative-ai", deserializeEmbeddingModel)
	provider.RegisterImageModelDeserializer("google", deserializeImageModel)
	provider.RegisterImageModelDeserializer("google.generative-ai", deserializeImageModel)
	provider.RegisterTranscriptionModelDeserializer("google.transcription", deserializeTranscriptionModel)
	provider.RegisterTranscriptionModelDeserializer("google.generative-ai.transcription", deserializeTranscriptionModel)
	provider.RegisterSpeechModelDeserializer("google.speech", deserializeSpeechModel)
	provider.RegisterSpeechModelDeserializer("google.generative-ai.speech", deserializeSpeechModel)
	provider.RegisterSpeechTranslationModelDeserializer("google.speech-translation", deserializeSpeechTranslationModel)
	provider.RegisterSpeechTranslationModelDeserializer("google.generative-ai.speech-translation", deserializeSpeechTranslationModel)
	provider.RegisterModelDeserializer("google.interactions", deserializeInteractionsModel)
	provider.RegisterModelDeserializer("google.generative-ai.interactions", deserializeInteractionsModel)
}

func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).LanguageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS google-embedding-model.ts [WORKFLOW_SERIALIZE].
func (m *EmbeddingModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeEmbeddingModel(serialized provider.SerializedModel) (provider.EmbeddingModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).EmbeddingModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS google-image-model.ts [WORKFLOW_SERIALIZE].
func (m *ImageModel) Serialize() provider.SerializedModel {
	prov := m.Provider()
	if m.prov == nil {
		return provider.SerializedModel{Provider: prov, ModelID: m.ModelID(), Config: map[string]interface{}{}}
	}
	return provider.SerializedModel{Provider: prov, ModelID: m.ModelID(), Config: provider.SerializableConfig(m.prov.config)}
}

func deserializeImageModel(serialized provider.SerializedModel) (provider.ImageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).ImageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS transcription/google-transcription-model.ts [WORKFLOW_SERIALIZE].
func (m *TranscriptionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.prov.config)}
}

func deserializeTranscriptionModel(serialized provider.SerializedModel) (provider.TranscriptionModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).TranscriptionModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS google-speech-model.ts [WORKFLOW_SERIALIZE].
//
// SpeechModel is shared with googlevertex (see
// pkg/providers/googlevertex/serialization.go), which tags both Gemini TTS
// (this same shared type, constructed with Kind: "gemini-tts") and Chirp
// 3: HD voices (googlevertex.CloudTTSSpeechModel) with the same
// "google.vertex.speech" Provider() string; the "kind" field below lets
// that package's deserializer pick the right Go type back. Google's own
// "google.speech" tag is unambiguous (no Chirp counterpart here), so kind
// is always empty and unused on this path.
func (m *SpeechModel) Serialize() provider.SerializedModel {
	cfg := map[string]interface{}{}
	if m.cfg.SerializableConfig != nil {
		if c := m.cfg.SerializableConfig(); c != nil {
			cfg = c
		}
	}
	if m.cfg.Kind != "" {
		cfg["kind"] = m.cfg.Kind
	}
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.modelID, Config: cfg}
}

func deserializeSpeechModel(serialized provider.SerializedModel) (provider.SpeechModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).SpeechModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS speech-translation/google-speech-translation-model.ts
// [WORKFLOW_SERIALIZE].
func (m *SpeechTranslationModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.prov.config)}
}

func deserializeSpeechTranslationModel(serialized provider.SerializedModel) (provider.SpeechTranslationModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).SpeechTranslationModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS interactions/google-interactions-language-model.ts
// [WORKFLOW_SERIALIZE], which adds a top-level "agent" alongside
// {modelId, config}; Go folds "agent" into Config since SerializedModel has
// no extra top-level slot.
func (m *InteractionsLanguageModel) Serialize() provider.SerializedModel {
	cfg := map[string]interface{}{}
	if m.cfg.SerializableConfig != nil {
		if c := m.cfg.SerializableConfig(); c != nil {
			cfg = c
		}
	}
	if m.agent != "" {
		cfg["agent"] = m.agent
	}
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.modelID, Config: cfg}
}

func deserializeInteractionsModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p := New(cfg)
	if agent, _ := serialized.Config["agent"].(string); agent != "" {
		return p.InteractionsAgent(agent)
	}
	return p.Interactions(serialized.ModelID)
}
