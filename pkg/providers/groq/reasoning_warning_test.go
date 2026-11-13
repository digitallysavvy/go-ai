package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS mapReasoningToProviderEffort warns when a level is coerced.
func TestReasoningCoercionWarning(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "openai/gpt-oss-20b")
	for level, want := range map[types.ReasoningLevel]string{types.ReasoningMinimal: "low", types.ReasoningXHigh: "high", types.ReasoningMax: "high"} {
		l := level
		body, warnings := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, Reasoning: &l}, false)
		if body["reasoning_effort"] != want {
			t.Fatalf("%s: effort = %v", level, body["reasoning_effort"])
		}
		found := false
		for _, w := range warnings {
			if w.Type == "compatibility" && w.Feature == "reasoning" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: missing compatibility warning: %#v", level, warnings)
		}
	}
	medium := types.ReasoningMedium
	_, warnings := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, Reasoning: &medium}, false)
	for _, w := range warnings {
		if w.Feature == "reasoning" {
			t.Fatalf("unexpected warning for medium: %#v", w)
		}
	}
}
