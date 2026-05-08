package gateway

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("gateway", deserializeModel)
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
