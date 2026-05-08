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
	if thinking["type"] != "adaptive" {
		t.Errorf("expected thinking type 'adaptive', got: %v", thinking["type"])
	}
	oc, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected output_config field, got: %v", body["output_config"])
	}
	if oc["effort"] != "medium" {
		t.Errorf("expected output_config.effort medium, got: %v", oc["effort"])
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
		level      types.ReasoningLevel
		wantType   string
		wantEffort string
	}{
		// claude-sonnet-4-6 supports adaptive thinking; xhigh maps to max.
		{types.ReasoningMinimal, "adaptive", "low"},
		{types.ReasoningLow, "adaptive", "low"},
		{types.ReasoningMedium, "adaptive", "medium"},
		{types.ReasoningHigh, "adaptive", "high"},
		{types.ReasoningXHigh, "adaptive", "max"},
		{types.ReasoningNone, "disabled", ""},
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
			if tt.wantEffort != "" {
				oc, ok := body["output_config"].(map[string]interface{})
				if !ok {
					t.Fatalf("expected output_config field, got: %v", body["output_config"])
				}
				if oc["effort"] != tt.wantEffort {
					t.Errorf("effort: want %v, got %v", tt.wantEffort, oc["effort"])
				}
			} else if _, ok := body["output_config"]; ok {
				t.Errorf("output_config should be omitted for %s", tt.level)
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
	if thinking["type"] != "adaptive" {
		t.Errorf("expected call-level adaptive thinking, got: %v", thinking["type"])
	}
	oc, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected output_config field, got: %v", body["output_config"])
	}
	if oc["effort"] != "medium" {
		t.Errorf("expected call-level effort medium, got: %v", oc["effort"])
	}
}
