package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22045: max is coerced to xhigh with a compatibility warning.
func TestPerplexityReasoningMaxMapsToXHigh(t *testing.T) {
	m := NewLanguageModel(CreatePerplexity(Config{APIKey: "k", BaseURL: "https://example.test"}), "")
	level := types.ReasoningMax
	body, warnings, err := m.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &level})
	if err != nil {
		t.Fatal(err)
	}
	if got := body["reasoning"].(map[string]interface{})["effort"]; got != "xhigh" {
		t.Fatalf("effort = %v, want xhigh", got)
	}
	found := false
	for _, w := range warnings {
		if w.Type == "compatibility" && w.Feature == "reasoning" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v", warnings)
	}

	xh := types.ReasoningXHigh
	body, warnings, _ = m.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &xh})
	if body["reasoning"].(map[string]interface{})["effort"] != "xhigh" {
		t.Fatal("xhigh should pass through")
	}
	for _, w := range warnings {
		if w.Feature == "reasoning" {
			t.Fatalf("unexpected warning %#v", w)
		}
	}
}
