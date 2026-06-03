package azure

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func init() {
	provider.RegisterModelDeserializer("azure-openai", deserializeModel)
	provider.RegisterModelDeserializer("azure.chat", deserializeModel)
	provider.RegisterModelDeserializer("azure.completion", deserializeCompletionModel)
	provider.RegisterModelDeserializer("azure.responses", deserializeResponsesModel)
}

func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).ChatModel(serialized.ModelID)
}

func deserializeResponsesModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg openai.Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return openai.New(cfg).ResponsesModel(serialized.ModelID)
}

func deserializeCompletionModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg openai.Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return openai.New(cfg).CompletionModel(serialized.ModelID)
}
