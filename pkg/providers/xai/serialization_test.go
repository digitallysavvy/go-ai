package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestSerializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://x.ai/api"})
	model, err := p.ChatCompletionsLanguageModel("grok-3")
	if err != nil {
		t.Fatalf("ChatCompletionsLanguageModel error = %v", err)
	}

	serialized := model.(*LanguageModel).Serialize()
	if serialized.Provider != "xai" {
		t.Fatalf("provider = %q", serialized.Provider)
	}
	if serialized.ModelID != "grok-3" {
		t.Fatalf("modelID = %q", serialized.ModelID)
	}
	if serialized.Config == nil {
		t.Fatal("expected serializable config")
	}
}

func TestSerializeResponsesLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.LanguageModel("grok-4")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}

	serialized := model.(*ResponsesLanguageModel).Serialize()
	if serialized.Provider != "xai.responses" {
		t.Fatalf("provider = %q", serialized.Provider)
	}
	if serialized.ModelID != "grok-4" {
		t.Fatalf("modelID = %q", serialized.ModelID)
	}
}

func TestDeserializeModel(t *testing.T) {
	respModel, err := deserializeModel(provider.SerializedModel{
		Provider: "xai.responses",
		ModelID:  "grok-4",
		Config:   map[string]interface{}{"apiKey": "key"},
	})
	if err != nil {
		t.Fatalf("deserialize responses error = %v", err)
	}
	if respModel.Provider() != "xai.responses" || respModel.ModelID() != "grok-4" {
		t.Fatalf("unexpected responses model: provider=%s model=%s", respModel.Provider(), respModel.ModelID())
	}

	chatModel, err := deserializeModel(provider.SerializedModel{
		Provider: "xai",
		ModelID:  "grok-3",
		Config:   map[string]interface{}{"apiKey": "key"},
	})
	if err != nil {
		t.Fatalf("deserialize chat error = %v", err)
	}
	if chatModel.Provider() != "xai" || chatModel.ModelID() != "grok-3" {
		t.Fatalf("unexpected chat model: provider=%s model=%s", chatModel.Provider(), chatModel.ModelID())
	}
}
