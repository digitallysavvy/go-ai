package fireworks

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS fireworks transformRequestBody: `...(thinking && {thinking})` forwards an
// empty object, `...(reasoningHistory && {...})` drops an empty string, and
// `promptCacheKey !== undefined` keeps an empty string.
func TestFireworksTruthinessOfFireworksOptions(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/kimi-k2p5")

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"fireworks": map[string]interface{}{
				"thinking":         map[string]interface{}{},
				"reasoningHistory": "",
				"promptCacheKey":   "",
			},
		},
	}, false)
	if th, ok := body["thinking"].(map[string]interface{}); !ok || len(th) != 0 {
		t.Errorf("thinking = %#v, want empty object", body["thinking"])
	}
	if _, ok := body["reasoning_history"]; ok {
		t.Errorf("empty reasoningHistory must not be forwarded: %#v", body["reasoning_history"])
	}
	if v, ok := body["prompt_cache_key"]; !ok || v != "" {
		t.Errorf("prompt_cache_key = %#v, want empty string kept", v)
	}

	unset := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	for _, k := range []string{"thinking", "reasoning_history", "prompt_cache_key", "service_tier"} {
		if _, ok := unset[k]; ok {
			t.Errorf("%s must stay absent when unset", k)
		}
	}
}
