package openresponses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS resolves `reasoningEffort ?? mapReasoningToProviderEffort(...)`: with the
// provider option set the mapping never runs, so reasoning "minimal" adds no
// compatibility warning.
func TestR47_ReasoningEffortOptionSkipsMappingWarning(t *testing.T) {
	m := NewLanguageModel(New(Config{BaseURL: "http://localhost:1234/v1"}), "gpt-5")
	minimal := types.ReasoningMinimal
	body, warnings, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		Reasoning:       &minimal,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "xhigh"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if body["reasoning"].(map[string]interface{})["effort"] != "xhigh" {
		t.Fatalf("reasoning = %v", body["reasoning"])
	}
	for _, w := range warnings {
		if w.Feature == "reasoning" {
			t.Fatalf("unexpected warning %+v", w)
		}
	}
	_, warnings, _ = m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &minimal}, false)
	found := false
	for _, w := range warnings {
		found = found || w.Feature == "reasoning"
	}
	if !found {
		t.Fatal("expected the mapping warning without the option")
	}
}
