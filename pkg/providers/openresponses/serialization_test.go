package openresponses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestOpenResponsesModelWorkflowSerializationRoundTrip covers P1-5c item 5:
// an Open Responses model without extensions round-trips through
// provider.SerializeModel/DeserializeModel like any other provider.
func TestOpenResponsesModelWorkflowSerializationRoundTrip(t *testing.T) {
	p := New(Config{BaseURL: "https://example.com/v1", APIKey: "secret"})
	model, err := p.LanguageModel("local-model")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}

	serialized, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel() error: %v", err)
	}
	if serialized.Provider != "open-responses.responses" || serialized.ModelID != "local-model" {
		t.Fatalf("unexpected serialized model: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}

	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel() error: %v", err)
	}
	if restored.Provider() != "open-responses.responses" || restored.ModelID() != "local-model" {
		t.Fatalf("unexpected restored model provider=%q model=%q", restored.Provider(), restored.ModelID())
	}
}

// TestOpenResponsesModelWithExtensionsCannotBeSerialized covers the
// WORKFLOW_SERIALIZE guard (row 9a68261 / P1-5c item 5): a model whose
// provider has registered extension codecs refuses to serialize, mirroring
// TS's static [WORKFLOW_SERIALIZE] throwing a SerializationError.
func TestOpenResponsesModelWithExtensionsCannotBeSerialized(t *testing.T) {
	p := New(Config{
		BaseURL: "https://example.com/v1",
		Extensions: []Extension{{
			ID:       "acme.widget",
			ToolType: "acme:widget",
			EncodeTool: func(string, map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{}, nil
			},
		}},
	})
	model, err := p.LanguageModel("local-model")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}

	_, err = provider.SerializeModel(model)
	if err == nil {
		t.Fatal("expected SerializeModel to fail for a model with registered extensions")
	}
	if !providererrors.IsSerializationError(err) {
		t.Fatalf("error = %#v, want a SerializationError", err)
	}
}
