package deepgram

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterSpeechModelDeserializer("deepgram", deserializeSpeechModel)
	provider.RegisterTranscriptionModelDeserializer("deepgram", deserializeTranscriptionModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS deepgram-speech-model.ts [WORKFLOW_SERIALIZE].
func (m *SpeechModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func deserializeSpeechModel(serialized provider.SerializedModel) (provider.SpeechModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return New(cfg).SpeechModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS deepgram-transcription-model.ts [WORKFLOW_SERIALIZE].
func (m *TranscriptionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}
}

func deserializeTranscriptionModel(serialized provider.SerializedModel) (provider.TranscriptionModel, error) {
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
