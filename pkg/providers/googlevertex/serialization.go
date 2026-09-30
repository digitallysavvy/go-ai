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
