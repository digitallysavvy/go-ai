package xai

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("xai", deserializeModel)
	provider.RegisterModelDeserializer("xai.responses", deserializeModel)
}

func (m *ResponsesLanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

// deserializeModel reconstructs a language model from a serialized
// workflow/agent state. The xAI Chat Completions API (and its "xai"-tagged
// serialized models) was removed (row 1f20dba); a model serialized with the
// legacy "xai" provider tag now deserializes to the Responses API model,
// matching LanguageModel()'s current (and only) behavior.
func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p := New(cfg)
	return p.LanguageModel(serialized.ModelID)
}
