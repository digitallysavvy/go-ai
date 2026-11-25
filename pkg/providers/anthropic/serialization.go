package anthropic

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func init() {
	// "anthropic" is the pre-TS-parity Provider() value; keep it so models
	// serialized by earlier versions still deserialize.
	provider.RegisterModelDeserializer("anthropic", deserializeModel)
	provider.RegisterModelDeserializer("anthropic.messages", deserializeModel)
	// Provider.ExtractionModel wraps a messages model (TS ExtractionLanguageModel
	// [WORKFLOW_SERIALIZE]).
	provider.RegisterExtractionModelDeserializer("anthropic.extraction", providerutils.DeserializeExtractionLanguageModel)
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
