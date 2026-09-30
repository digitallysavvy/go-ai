package prodia

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("prodia.language", deserializeLanguageModel)
	provider.RegisterImageModelDeserializer("prodia.image", deserializeImageModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS prodia-language-model.ts [WORKFLOW_SERIALIZE]. Prodia's video
// model (ProdiaVideoModel) has no WORKFLOW_SERIALIZE in TS, so it is
// intentionally not wired up here.
func (m *ProdiaLanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.prov.config),
	}
}

func deserializeLanguageModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
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

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS prodia-image-model.ts [WORKFLOW_SERIALIZE].
func (m *ImageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.prov.config),
	}
}

func deserializeImageModel(serialized provider.SerializedModel) (provider.ImageModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).ImageModel(serialized.ModelID)
}
