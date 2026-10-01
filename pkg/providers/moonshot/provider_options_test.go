package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestMoonshotCamelCaseProviderOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "kimi-k2.6")

	opts := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"moonshot": map[string]interface{}{
				"thinking": map[string]interface{}{
					"type":         "enabled",
					"budgetTokens": 2048,
				},
				"reasoningHistory": "preserved",
			},
		},
	}
	body, warnings, err := model.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// budgetTokens is deprecated on Moonshot Chat Completions: it must be
	// omitted from the wire body and produce a deprecation warning.
	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking object in body, got %#v", body["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Errorf("expected thinking.type=enabled, got %v", thinking["type"])
	}
	if _, hasBudget := thinking["budget_tokens"]; hasBudget {
		t.Errorf("expected budget_tokens to be omitted from the wire body, got %v", thinking["budget_tokens"])
	}
	if thinking["keep"] != "all" {
		t.Errorf("expected reasoningHistory=preserved to map to thinking.keep=all, got %v", thinking["keep"])
	}
	if _, has := body["reasoning_history"]; has {
		t.Errorf("expected reasoning_history to never be sent on the wire, got %v", body["reasoning_history"])
	}

	foundDeprecated := false
	for _, w := range warnings {
		if w.Type == "deprecated" && w.Setting == "providerOptions.moonshot.thinking.budgetTokens" {
			foundDeprecated = true
		}
	}
	if !foundDeprecated {
		t.Errorf("expected a deprecated budgetTokens warning, got %#v", warnings)
	}
}

func TestMoonshotSnakeCaseThinkingKeysRejected(t *testing.T) {
	// The current provider options schema only recognizes "type" (camelCase
	// keys only, no legacy snake/kebab aliases for thinking sub-fields).
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "kimi-k2.6")

	opts := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"moonshot": map[string]interface{}{
				"thinking": map[string]interface{}{
					"type": "disabled",
				},
			},
		},
	}
	body, _, err := model.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("expected thinking.type=disabled, got %#v", body["thinking"])
	}
}
