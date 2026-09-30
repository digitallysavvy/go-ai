package gladia

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterTranscriptionModelDeserializer("gladia", deserializeModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS gladia-transcription-model.ts [WORKFLOW_SERIALIZE].
func (m *TranscriptionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func deserializeModel(serialized provider.SerializedModel) (provider.TranscriptionModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).TranscriptionModel(serialized.ModelID)
}
