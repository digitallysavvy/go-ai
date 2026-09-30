package bedrock

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("amazon-bedrock", deserializeModel)
	provider.RegisterModelDeserializer("bedrock", deserializeModel)
	provider.RegisterModelDeserializer("aws-bedrock", deserializeModel)
	provider.RegisterEmbeddingModelDeserializer("amazon-bedrock", deserializeEmbeddingModel)
	provider.RegisterImageModelDeserializer("amazon-bedrock", deserializeImageModel)
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

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS amazon-bedrock-embedding-model.ts [WORKFLOW_SERIALIZE].
func (m *EmbeddingModel) Serialize() provider.SerializedModel {
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

func deserializeEmbeddingModel(serialized provider.SerializedModel) (provider.EmbeddingModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	var opts *EmbeddingOptions
	if raw, ok := serialized.Config["modelOptions"]; ok {
		opts = &EmbeddingOptions{}
		data, _ := json.Marshal(raw)
		_ = json.Unmarshal(data, opts)
	}
	return New(cfg).EmbeddingModelWithOptions(serialized.ModelID, opts)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS amazon-bedrock-image-model.ts [WORKFLOW_SERIALIZE].
func (m *ImageModel) Serialize() provider.SerializedModel {
	cfg := provider.SerializableConfig(m.provider.config)
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: cfg}
}

func deserializeImageModel(serialized provider.SerializedModel) (provider.ImageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return New(cfg).ImageModel(serialized.ModelID)
}
