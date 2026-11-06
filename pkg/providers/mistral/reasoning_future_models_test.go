package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22187 and #22494: Large 4 and unknown models accept reasoning
// configuration; only known non-reasoning models warn.
func TestMistralReasoningSupportForFutureModels(t *testing.T) {
	high := types.ReasoningHigh
	for _, id := range []string{ModelMistralLarge4, ModelMistralLarge40, "magistral-large-latest", "mistral-large-5", "future-reasoning-model"} {
		lm := NewLanguageModel(New(Config{APIKey: "k"}), id)
		if ws := lm.checkReasoningWarnings(&provider.GenerateOptions{Reasoning: &high}); len(ws) != 0 {
			t.Errorf("%s: unexpected warnings %v", id, ws)
		}
		body := lm.buildRequestBody(&provider.GenerateOptions{Reasoning: &high, Prompt: types.Prompt{Text: "x"}}, false)
		if body["reasoning_effort"] != "high" {
			t.Errorf("%s: reasoning_effort = %v", id, body["reasoning_effort"])
		}
		body = lm.buildRequestBody(&provider.GenerateOptions{
			Reasoning: &high, Prompt: types.Prompt{Text: "x"},
			ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"reasoningEffort": "none"}},
		}, false)
		if body["reasoning_effort"] != "none" {
			t.Errorf("%s: override reasoning_effort = %v", id, body["reasoning_effort"])
		}
	}
	for id := range mistralModelsWithoutReasoningEffort {
		lm := NewLanguageModel(New(Config{APIKey: "k"}), id)
		if ws := lm.checkReasoningWarnings(&provider.GenerateOptions{Reasoning: &high}); len(ws) != 1 {
			t.Errorf("%s: want one reasoning warning, got %v", id, ws)
		}
	}
}
