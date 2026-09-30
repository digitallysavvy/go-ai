package gmicloud

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("gmicloud", deserializeModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS gmicloud-chat-language-model.ts [WORKFLOW_SERIALIZE].
func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
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
