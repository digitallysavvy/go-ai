package xai

import (
	"encoding/json"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("xai", deserializeModel)
	provider.RegisterModelDeserializer("xai.responses", deserializeModel)
}

func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func (m *ResponsesLanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p := New(cfg)
	if strings.HasSuffix(serialized.Provider, ".responses") {
		return p.LanguageModel(serialized.ModelID)
	}
	return p.ChatCompletionsLanguageModel(serialized.ModelID)
}
