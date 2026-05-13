package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestOpenAISerializeAndDeserializeModels(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.openai.com/v1"})

	chatAny, err := p.LanguageModel("gpt-4.1")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	chatSerialized := chatAny.(*LanguageModel).Serialize()
	if chatSerialized.Provider != "openai" || chatSerialized.ModelID != "gpt-4.1" || chatSerialized.Config == nil {
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

	deserializedChat, err := deserializeModel(provider.SerializedModel{
		Provider: "openai",
		ModelID:  "gpt-4.1-mini",
		Config:   map[string]interface{}{"apiKey": "k"},
	})
	if err != nil {
		t.Fatalf("deserialize chat error = %v", err)
	}
	if deserializedChat.Provider() != "openai" || deserializedChat.ModelID() != "gpt-4.1-mini" {
		t.Fatalf("deserialized chat mismatch: provider=%s model=%s", deserializedChat.Provider(), deserializedChat.ModelID())
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
}
