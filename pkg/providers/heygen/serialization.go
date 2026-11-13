package heygen

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterVideoModelDeserializer("heygen.video", deserializeVideoModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS heygen-video-model.ts [WORKFLOW_SERIALIZE]. Credentials are not
// serialized; the restored model reads HEYGEN_API_KEY.
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
