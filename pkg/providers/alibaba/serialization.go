package alibaba

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("alibaba", deserializeLanguageModel)
	provider.RegisterEmbeddingModelDeserializer("alibaba.embedding", deserializeEmbeddingModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS alibaba-chat-language-model.ts [WORKFLOW_SERIALIZE].
func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.prov.config),
	}
}

func deserializeLanguageModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).LanguageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS alibaba-embedding-model.ts [WORKFLOW_SERIALIZE].
func (m *EmbeddingModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func deserializeEmbeddingModel(serialized provider.SerializedModel) (provider.EmbeddingModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).EmbeddingModel(serialized.ModelID)
}
