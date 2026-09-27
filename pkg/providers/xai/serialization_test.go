package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

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

	// A model serialized with the legacy "xai" (Chat Completions) provider
	// tag now deserializes to the Responses API model (row 1f20dba: Chat
	// Completions was removed; LanguageModel() is Responses-only).
	legacyTaggedModel, err := deserializeModel(provider.SerializedModel{
		Provider: "xai",
		ModelID:  "grok-3",
		Config:   map[string]interface{}{"apiKey": "key"},
	})
	if err != nil {
		t.Fatalf("deserialize legacy-tagged model error = %v", err)
	}
	if legacyTaggedModel.Provider() != "xai.responses" || legacyTaggedModel.ModelID() != "grok-3" {
		t.Fatalf("unexpected legacy-tagged model: provider=%s model=%s", legacyTaggedModel.Provider(), legacyTaggedModel.ModelID())
	}
}
