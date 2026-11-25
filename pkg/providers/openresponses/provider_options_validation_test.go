package openresponses

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestOpenResponsesReasoningOptionsAreValidated(t *testing.T) {
	m := NewLanguageModel(New(Config{BaseURL: "http://localhost:1234/v1"}), "m")
	for _, opts := range []map[string]interface{}{
		{"reasoningSummary": "legacy"},
		{"reasoningSummary": 1},
		{"reasoningEffort": 5},
	} {
		_, _, err := m.buildRequestBody(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"openai": opts},
		}, false)
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) {
			t.Fatalf("%v: err = %v, want InvalidArgumentError", opts, err)
		}
	}
	if _, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningSummary": nil, "reasoningEffort": nil}},
	}, false); err != nil {
		t.Fatalf("null options rejected: %v", err)
	}
}
