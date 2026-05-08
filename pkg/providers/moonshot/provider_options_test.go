package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestMoonshotCamelCaseProviderOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "kimi-k2")

	opts := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"moonshot": map[string]interface{}{
				"thinking": map[string]interface{}{
					"type":         "enabled",
					"budgetTokens": 2048,
				},
				"reasoningHistory": "keep",
			},
		},
	}
	body, warnings := model.buildRequestBodyWithWarnings(opts, false)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for camelCase provider options, got %#v", warnings)
	}
	thinking := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Errorf("expected thinking.type=enabled, got %v", thinking["type"])
	}
	if thinking["budget_tokens"] != 2048 {
		t.Errorf("expected budgetTokens to serialize as budget_tokens=2048, got %v", thinking["budget_tokens"])
	}
	if body["reasoning_history"] != "keep" {
		t.Errorf("expected reasoningHistory to serialize as reasoning_history, got %v", body["reasoning_history"])
	}
}
