package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
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
}
