package openai

import (
	"encoding/json"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("openai", deserializeModel)
	provider.RegisterModelDeserializer("openai.chat", deserializeModel)
	provider.RegisterModelDeserializer("openai.completion", deserializeModel)
	provider.RegisterModelDeserializer("openai.responses", deserializeModel)
	provider.RegisterEmbeddingModelDeserializer("openai", deserializeEmbeddingModel)
	provider.RegisterImageModelDeserializer("openai", deserializeImageModel)
	provider.RegisterSpeechModelDeserializer("openai", deserializeSpeechModel)
	provider.RegisterTranscriptionModelDeserializer("openai", deserializeTranscriptionModel)
	provider.RegisterSpeechTranslationModelDeserializer("openai.speech-translation", deserializeSpeechTranslationModel)
}

func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func (m *ResponsesLanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func (m *CompletionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p := New(cfg)
	if strings.HasSuffix(serialized.Provider, ".responses") {
		return p.ResponsesModel(serialized.ModelID)
	}
	if strings.HasSuffix(serialized.Provider, ".chat") {
		return p.ChatModel(serialized.ModelID)
	}
	if strings.HasSuffix(serialized.Provider, ".completion") {
		return p.CompletionModel(serialized.ModelID)
	}
	return p.LanguageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS openai-embedding-model.ts [WORKFLOW_SERIALIZE].
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
// Mirrors TS openai-image-model.ts [WORKFLOW_SERIALIZE].
func (m *ImageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeImageModel(serialized provider.SerializedModel) (provider.ImageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).ImageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS openai-speech-model.ts [WORKFLOW_SERIALIZE].
func (m *SpeechModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeSpeechModel(serialized provider.SerializedModel) (provider.SpeechModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).SpeechModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS openai-transcription-model.ts [WORKFLOW_SERIALIZE].
func (m *TranscriptionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeTranscriptionModel(serialized provider.SerializedModel) (provider.TranscriptionModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).TranscriptionModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS speech-translation/openai-speech-translation-model.ts
// [WORKFLOW_SERIALIZE].
func (m *SpeechTranslationModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeSpeechTranslationModel(serialized provider.SerializedModel) (provider.SpeechTranslationModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).SpeechTranslationModel(serialized.ModelID)
}
