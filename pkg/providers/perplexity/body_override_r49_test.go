package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs spreads agentOptions first and then assigns max_output_tokens,
// temperature, top_p and response_format explicitly, so a loose
// providerOptions.perplexity entry with one of those names is overridden by
// the (undefined) call setting and never sent.
func TestPerplexityLooseOptionsDoNotOverrideStandardBodyKeys(t *testing.T) {
	var captured map[string]interface{}
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, &captured))

	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: testPrompt},
		ProviderOptions: map[string]interface{}{"perplexity": map[string]interface{}{
			"temperature":       0.9,
			"top_p":             0.5,
			"max_output_tokens": 77,
			"response_format":   map[string]interface{}{"type": "text"},
			"custom_flag":       true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"temperature", "top_p", "max_output_tokens", "response_format"} {
		if v, ok := captured[k]; ok {
			t.Errorf("%s = %v, want absent", k, v)
		}
	}
	if captured["custom_flag"] != true {
		t.Errorf("custom_flag = %v, want passthrough", captured["custom_flag"])
	}
}
