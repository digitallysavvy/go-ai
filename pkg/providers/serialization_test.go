package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func TestOpenAIModelWorkflowSerializationRoundTrip(t *testing.T) {
	p := openai.New(openai.Config{
		APIKey:  "secret",
		BaseURL: "https://example.com/v1",
		Headers: map[string]string{
			"Authorization": "Bearer secret",
		},
	})
	model, err := p.LanguageModel("gpt-4o")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}

	serializable, ok := model.(provider.SerializableModel)
	if !ok {
		t.Fatalf("model does not implement provider.SerializableModel")
	}
	serialized := serializable.Serialize()
	if serialized.Provider != "openai" || serialized.ModelID != "gpt-4o" {
		t.Fatalf("unexpected serialized model: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["Headers"]; ok {
		t.Fatalf("Headers should not be serialized: %#v", serialized.Config)
	}

	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel() error: %v", err)
	}
	if restored.Provider() != "openai" || restored.ModelID() != "gpt-4o" {
		t.Fatalf("unexpected restored model provider=%q model=%q", restored.Provider(), restored.ModelID())
	}

	asMap, err := providerutils.SerializeModel(model)
	if err != nil {
		t.Fatalf("providerutils.SerializeModel() error: %v", err)
	}
	if asMap["provider"] != "openai" || asMap["modelId"] != "gpt-4o" {
		t.Fatalf("unexpected providerutils serialization: %#v", asMap)
	}
}

func TestAnthropicModelWorkflowSerializationRoundTrip(t *testing.T) {
	p := anthropic.New(anthropic.Config{
		APIKey:  "secret",
		BaseURL: "https://example.com",
		Headers: map[string]string{
			"x-api-key": "secret",
		},
	})
	model, err := p.LanguageModel("claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}

	serialized, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel() error: %v", err)
	}
	if serialized.Provider != "anthropic" || serialized.ModelID != "claude-sonnet-4-20250514" {
		t.Fatalf("unexpected serialized model: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["Headers"]; ok {
		t.Fatalf("Headers should not be serialized: %#v", serialized.Config)
	}

	restored, err := providerutils.DeserializeModel(serialized.Provider, serialized.ModelID, serialized.Config)
	if err != nil {
		t.Fatalf("DeserializeModel() error: %v", err)
	}
	if restored.Provider() != "anthropic" || restored.ModelID() != "claude-sonnet-4-20250514" {
		t.Fatalf("unexpected restored model provider=%q model=%q", restored.Provider(), restored.ModelID())
	}
}
