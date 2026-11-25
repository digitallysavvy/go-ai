package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS moonshotaiLanguageModelOptions: thinking.budgetTokens is
// z.number().int().min(1024), and every member is strictly typed; a violation
// fails the call instead of being ignored.
func TestR37_InvalidModelOptionsFail(t *testing.T) {
	model := newMoonshotTestModel("kimi-k2.6")
	for name, opts := range map[string]map[string]interface{}{
		"budget below 1024":    {"thinking": map[string]interface{}{"budgetTokens": 100}},
		"budget fractional":    {"thinking": map[string]interface{}{"budgetTokens": 2048.5}},
		"budget not a number":  {"thinking": map[string]interface{}{"budgetTokens": "2048"}},
		"thinking not object":  {"thinking": "enabled"},
		"thinking type number": {"thinking": map[string]interface{}{"type": 1}},
		"strict not bool":      {"strictJsonSchema": "false"},
		"logprobs not bool":    {"logprobs": 1},
		"effort not string":    {"reasoningEffort": 5},
		"history not string":   {"reasoningHistory": true},
		"cache key not string": {"promptCacheKey": 7},
		"safety id not string": {"safetyIdentifier": false},
	} {
		_, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:          textPrompt("Hello"),
			ProviderOptions: map[string]interface{}{"moonshot": opts},
		}, false)
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// A valid budget still only warns.
	if _, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          textPrompt("Hello"),
		ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"thinking": map[string]interface{}{"budgetTokens": 1024}}},
	}, false); err != nil {
		t.Errorf("valid budget: %v", err)
	}
}
