package openai

import (
	"encoding/json"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("openai", deserializeModel)
	provider.RegisterModelDeserializer("openai.chat", deserializeModel)
	provider.RegisterModelDeserializer("openai.completion", deserializeModel)
	provider.RegisterModelDeserializer("openai.responses", deserializeModel)
}

func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func (m *ResponsesLanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func (m *CompletionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p := New(cfg)
	if strings.HasSuffix(serialized.Provider, ".responses") {
		return p.ResponsesModel(serialized.ModelID)
	}
	if strings.HasSuffix(serialized.Provider, ".chat") {
		return p.ChatModel(serialized.ModelID)
	}
	if strings.HasSuffix(serialized.Provider, ".completion") {
		return p.CompletionModel(serialized.ModelID)
	}
	return p.LanguageModel(serialized.ModelID)
}
