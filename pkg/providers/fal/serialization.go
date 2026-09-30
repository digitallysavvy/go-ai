package fal

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterImageModelDeserializer("fal", deserializeImageModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS fal-image-model.ts [WORKFLOW_SERIALIZE].
//
// NOTE: TS also gives fal-speech-model.ts and fal-transcription-model.ts
// WORKFLOW_SERIALIZE, but those Go model types don't exist in this package
// yet (tracked separately as B1-3: FalSpeechModel/FalTranscriptionModel).
// Once they land, add matching Serialize()/RegisterSpeechModelDeserializer/
// RegisterTranscriptionModelDeserializer here following this same pattern.
func (m *ImageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
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
