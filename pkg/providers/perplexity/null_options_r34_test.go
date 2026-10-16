package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// zod .optional() accepts undefined but not null, so an explicit null for a
// modeled perplexity option fails parseProviderOptions in TS.
func TestNullAgentOptionsRejectedR34(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k"}), "openai/gpt-5.1")
	cases := map[string]map[string]interface{}{
		"instructions":           {"instructions": nil},
		"store":                  {"store": nil},
		"max_steps":              {"max_steps": nil},
		"reasoning":              {"reasoning": nil},
		"reasoning.effort":       {"reasoning": map[string]interface{}{"effort": nil}},
		"web_search.filters":     {"tools": []interface{}{map[string]interface{}{"type": "web_search", "filters": nil}}},
		"web_search.max_results": {"tools": []interface{}{map[string]interface{}{"type": "web_search", "max_results": nil}}},
		"web_search.size":        {"tools": []interface{}{map[string]interface{}{"type": "web_search", "search_context_size": nil}}},
	}
	for name, opts := range cases {
		_, _, err := model.getArgs(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"perplexity": opts},
		})
		if err == nil {
			t.Errorf("%s: expected an invalid-option error for null", name)
		}
	}
	// Omitted options are still fine.
	if _, _, err := model.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: map[string]interface{}{"perplexity": map[string]interface{}{}}}); err != nil {
		t.Errorf("empty options: %v", err)
	}
}
