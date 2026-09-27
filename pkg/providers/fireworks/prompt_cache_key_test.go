package fireworks

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported (behaviorally) from ai/packages/fireworks/src/fireworks-chat-options.ts's
// promptCacheKey/serviceTier options (7cf47d8, e646e19).

func TestFireworks_PromptCacheKeyAndServiceTierForwarded(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/kimi-k2p5")

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"fireworks": map[string]interface{}{
				"promptCacheKey": "my-cache-key",
				"serviceTier":    "priority",
			},
		},
	}, false)

	if body["prompt_cache_key"] != "my-cache-key" {
		t.Errorf("prompt_cache_key = %#v", body["prompt_cache_key"])
	}
	if body["service_tier"] != "priority" {
		t.Errorf("service_tier = %#v", body["service_tier"])
	}
}

func TestFireworks_ThinkingUnderTopLevelKeyIsIgnored(t *testing.T) {
	// providerOptions.thinking (top-level, not namespaced under "fireworks")
	// is not a Fireworks option and must not leak into the request body.
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/kimi-k2p5")

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"thinking": map[string]interface{}{"type": "enabled"},
		},
	}, false)

	if _, ok := body["thinking"]; ok {
		t.Errorf("expected top-level (non-namespaced) thinking to be ignored, got %#v", body["thinking"])
	}
}
