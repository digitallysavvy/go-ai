package bedrock

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("bedrock", deserializeModel)
}

func (m *LanguageModel) Serialize() provider.SerializedModel {
	cfg := provider.SerializableConfig(m.provider.config)
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	if m.options != nil {
		data, _ := json.Marshal(m.options)
		var opts map[string]interface{}
		_ = json.Unmarshal(data, &opts)
		cfg["modelOptions"] = opts
	}
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: cfg}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	var opts *ModelOptions
	if raw, ok := serialized.Config["modelOptions"]; ok {
		opts = &ModelOptions{}
		data, _ := json.Marshal(raw)
		_ = json.Unmarshal(data, opts)
	}
	return New(cfg).LanguageModelWithOptions(serialized.ModelID, opts)
}
