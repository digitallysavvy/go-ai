package voyage

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterEmbeddingModelDeserializer("voyage.embedding", deserializeModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS voyage-embedding-model.ts [WORKFLOW_SERIALIZE].
func (m *EmbeddingModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func deserializeModel(serialized provider.SerializedModel) (provider.EmbeddingModel, error) {
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
