package topaz

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterImageModelDeserializer("topaz.image", deserializeImageModel)
	provider.RegisterVideoModelDeserializer("topaz.video", deserializeVideoModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS topaz-image-model.ts [WORKFLOW_SERIALIZE].
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

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS topaz-video-model.ts [WORKFLOW_SERIALIZE].
func (m *VideoModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.prov.config),
	}
}

func deserializeVideoModel(serialized provider.SerializedModel) (provider.VideoModelV3, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).VideoModel(serialized.ModelID)
}
