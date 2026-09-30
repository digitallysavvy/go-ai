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
