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
	provider.RegisterEmbeddingModelDeserializer("azure.embeddings", deserializeEmbeddingModel)
	provider.RegisterImageModelDeserializer("azure.image", deserializeImageModel)
	provider.RegisterSpeechModelDeserializer("azure.speech", deserializeSpeechModel)
	provider.RegisterTranscriptionModelDeserializer("azure.transcription", deserializeTranscriptionModel)
}

func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.ChatModel(serialized.ModelID)
}

func deserializeResponsesModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg openai.Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return openai.NewResponsesLanguageModel(openai.New(cfg), serialized.ModelID), nil
}

func deserializeCompletionModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg openai.Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	return openai.NewCompletionModel(openai.New(cfg), serialized.ModelID), nil
}

// Serialize implements provider.SerializableModel for workflow boundaries.
func (m *EmbeddingModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeEmbeddingModel(serialized provider.SerializedModel) (provider.EmbeddingModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.EmbeddingModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
func (m *ImageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeImageModel(serialized provider.SerializedModel) (provider.ImageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.ImageModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
func (m *SpeechModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeSpeechModel(serialized provider.SerializedModel) (provider.SpeechModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.SpeechModel(serialized.ModelID)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
func (m *TranscriptionModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeTranscriptionModel(serialized provider.SerializedModel) (provider.TranscriptionModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.TranscriptionModel(serialized.ModelID)
}
