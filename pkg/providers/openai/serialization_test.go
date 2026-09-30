package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestOpenAISerializeAndDeserializeModels(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.openai.com/v1"})

	defaultAny, err := p.LanguageModel("gpt-4.1")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if defaultAny.Provider() != "openai.responses" {
		t.Fatalf("LanguageModel provider = %q, want openai.responses", defaultAny.Provider())
	}

	chatAny, err := p.ChatModel("gpt-4.1")
	if err != nil {
		t.Fatalf("ChatModel error = %v", err)
	}
	chatSerialized := chatAny.(*LanguageModel).Serialize()
	if chatSerialized.Provider != "openai.chat" || chatSerialized.ModelID != "gpt-4.1" || chatSerialized.Config == nil {
		t.Fatalf("chat serialize mismatch: %#v", chatSerialized)
	}

	respAny, err := p.ResponsesModel("gpt-4.1")
	if err != nil {
		t.Fatalf("ResponsesModel error = %v", err)
	}
	respSerialized := respAny.(*ResponsesLanguageModel).Serialize()
	if respSerialized.Provider != "openai.responses" || respSerialized.ModelID != "gpt-4.1" {
		t.Fatalf("responses serialize mismatch: %#v", respSerialized)
	}

	completionAny, err := p.CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatalf("CompletionModel error = %v", err)
	}
	completionSerialized := completionAny.(*CompletionModel).Serialize()
	if completionSerialized.Provider != "openai.completion" || completionSerialized.ModelID != "gpt-3.5-turbo-instruct" {
		t.Fatalf("completion serialize mismatch: %#v", completionSerialized)
	}

	deserializedChat, err := deserializeModel(provider.SerializedModel{
		Provider: "openai.chat",
		ModelID:  "gpt-4.1-mini",
		Config:   map[string]interface{}{"apiKey": "k"},
	})
	if err != nil {
		t.Fatalf("deserialize chat error = %v", err)
	}
	if deserializedChat.Provider() != "openai.chat" || deserializedChat.ModelID() != "gpt-4.1-mini" {
		t.Fatalf("deserialized chat mismatch: provider=%s model=%s", deserializedChat.Provider(), deserializedChat.ModelID())
	}

	deserializedDefault, err := deserializeModel(provider.SerializedModel{
		Provider: "openai",
		ModelID:  "gpt-4.1-mini",
		Config:   map[string]interface{}{"apiKey": "k"},
	})
	if err != nil {
		t.Fatalf("deserialize default error = %v", err)
	}
	if deserializedDefault.Provider() != "openai.responses" || deserializedDefault.ModelID() != "gpt-4.1-mini" {
		t.Fatalf("deserialized default mismatch: provider=%s model=%s", deserializedDefault.Provider(), deserializedDefault.ModelID())
	}

	deserializedResp, err := deserializeModel(provider.SerializedModel{
		Provider: "openai.responses",
		ModelID:  "gpt-4.1-mini",
		Config:   map[string]interface{}{"apiKey": "k"},
	})
	if err != nil {
		t.Fatalf("deserialize responses error = %v", err)
	}
	if deserializedResp.Provider() != "openai.responses" || deserializedResp.ModelID() != "gpt-4.1-mini" {
		t.Fatalf("deserialized responses mismatch: provider=%s model=%s", deserializedResp.Provider(), deserializedResp.ModelID())
	}

	deserializedCompletion, err := deserializeModel(provider.SerializedModel{
		Provider: "openai.completion",
		ModelID:  "gpt-3.5-turbo-instruct",
		Config:   map[string]interface{}{"apiKey": "k"},
	})
	if err != nil {
		t.Fatalf("deserialize completion error = %v", err)
	}
	if deserializedCompletion.Provider() != "openai.completion" || deserializedCompletion.ModelID() != "gpt-3.5-turbo-instruct" {
		t.Fatalf("deserialized completion mismatch: provider=%s model=%s", deserializedCompletion.Provider(), deserializedCompletion.ModelID())
	}
}

func TestOpenAISerializeAndDeserializeEmbeddingModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.openai.com/v1"})
	modelAny, err := p.EmbeddingModel("text-embedding-3-small")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := modelAny.(*EmbeddingModel).Serialize()
	if serialized.Provider != "openai" || serialized.ModelID != "text-embedding-3-small" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "openai" || restored.ModelID() != "text-embedding-3-small" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "text-embedding-3-small" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestOpenAISerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.openai.com/v1"})
	modelAny, err := p.ImageModel("dall-e-3")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := modelAny.(*ImageModel).Serialize()
	if serialized.Provider != "openai" || serialized.ModelID != "dall-e-3" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "openai" || restored.ModelID() != "dall-e-3" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "dall-e-3" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestOpenAISerializeAndDeserializeSpeechModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.openai.com/v1"})
	modelAny, err := p.SpeechModel("tts-1")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := modelAny.(*SpeechModel).Serialize()
	if serialized.Provider != "openai" || serialized.ModelID != "tts-1" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("deserializeSpeechModel error = %v", err)
	}
	if restored.Provider() != "openai" || restored.ModelID() != "tts-1" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeSpeechModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeSpeechModel error = %v", err)
	}
	if viaRegistry.ModelID() != "tts-1" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestOpenAISerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.openai.com/v1"})
	modelAny, err := p.TranscriptionModel("whisper-1")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := modelAny.(*TranscriptionModel).Serialize()
	if serialized.Provider != "openai" || serialized.ModelID != "whisper-1" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "openai" || restored.ModelID() != "whisper-1" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != "whisper-1" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
