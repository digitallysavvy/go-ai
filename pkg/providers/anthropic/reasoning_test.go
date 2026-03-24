package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func makeTestProvider() *Provider {
	return New(Config{APIKey: "test-key", BaseURL: DefaultBaseURL})
}

func TestAnthropicReasoningMedium(t *testing.T) {
	prov := makeTestProvider()
	model := NewLanguageModel(prov, "claude-sonnet-4-6", nil)

	level := types.ReasoningMedium
	opts := &provider.GenerateOptions{
		Reasoning: &level,
	}

	body := model.buildRequestBody(opts, false)

	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'thinking' field in request body, got: %v", body["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Errorf("expected thinking type 'enabled', got: %v", thinking["type"])
	}
	if thinking["budget_tokens"] != 10000 {
		t.Errorf("expected budget_tokens 10000, got: %v", thinking["budget_tokens"])
	}
}

func TestAnthropicReasoningNone(t *testing.T) {
	prov := makeTestProvider()
	model := NewLanguageModel(prov, "claude-sonnet-4-6", nil)

	level := types.ReasoningNone
	opts := &provider.GenerateOptions{
		Reasoning: &level,
	}

	body := model.buildRequestBody(opts, false)

	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'thinking' field, got: %v", body["thinking"])
	}
	if thinking["type"] != "disabled" {
		t.Errorf("expected thinking type 'disabled', got: %v", thinking["type"])
	}
	if _, hasBudget := thinking["budget_tokens"]; hasBudget {
		t.Error("expected no budget_tokens for disabled thinking")
	}
}

func TestAnthropicReasoningDefault(t *testing.T) {
	prov := makeTestProvider()
	model := NewLanguageModel(prov, "claude-sonnet-4-6", nil)

	level := types.ReasoningDefault
	opts := &provider.GenerateOptions{
		Reasoning: &level,
	}

	body := model.buildRequestBody(opts, false)

	// provider-default → no thinking field set
	if _, hasThinking := body["thinking"]; hasThinking {
		t.Errorf("expected no 'thinking' field for provider-default, got: %v", body["thinking"])
	}
}

func TestAnthropicReasoningAllLevels(t *testing.T) {
	prov := makeTestProvider()
	model := NewLanguageModel(prov, "claude-sonnet-4-6", nil)

	tests := []struct {
		level         types.ReasoningLevel
		wantType      string
		wantBudget    interface{}
		hasBudget     bool
	}{
		{types.ReasoningMinimal, "enabled", 1024, true},
		{types.ReasoningLow, "enabled", 4000, true},
		{types.ReasoningMedium, "enabled", 10000, true},
		{types.ReasoningHigh, "enabled", 16000, true},
		{types.ReasoningXHigh, "enabled", 32000, true},
		{types.ReasoningNone, "disabled", nil, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			opts := &provider.GenerateOptions{Reasoning: &level}
			body := model.buildRequestBody(opts, false)

			thinking, ok := body["thinking"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected 'thinking' field, got: %v", body["thinking"])
			}
			if thinking["type"] != tt.wantType {
				t.Errorf("type: want %q, got %v", tt.wantType, thinking["type"])
			}
			if tt.hasBudget {
				if thinking["budget_tokens"] != tt.wantBudget {
					t.Errorf("budget_tokens: want %v, got %v", tt.wantBudget, thinking["budget_tokens"])
				}
			}
		})
	}
}

// TestAnthropicReasoningOverridesModelOption verifies that call-level Reasoning
// takes precedence over the model-level Thinking option.
func TestAnthropicReasoningOverridesModelOption(t *testing.T) {
	prov := makeTestProvider()
	budget := 5000
	modelOpts := &ModelOptions{
		Thinking: &ThinkingConfig{
			Type:         ThinkingTypeEnabled,
			BudgetTokens: &budget,
		},
	}
	model := NewLanguageModel(prov, "claude-sonnet-4-6", modelOpts)

	// Call-level medium should override model-level 5000
	level := types.ReasoningMedium
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'thinking' field, got: %v", body["thinking"])
	}
	if thinking["budget_tokens"] != 10000 {
		t.Errorf("expected call-level budget_tokens 10000, got: %v", thinking["budget_tokens"])
	}
}

