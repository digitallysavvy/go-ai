package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func newBedrockClaudeModel() *LanguageModel {
	p := New(Config{
		AWSAccessKeyID:     "test-key",
		AWSSecretAccessKey: "test-secret",
		Region:             "us-east-1",
	})
	return NewLanguageModel(p, "anthropic.claude-3-5-sonnet-20241022-v2:0")
}

func newBedrockNovaModel() *LanguageModel {
	p := New(Config{
		AWSAccessKeyID:     "test-key",
		AWSSecretAccessKey: "test-secret",
		Region:             "us-east-1",
	})
	return NewLanguageModel(p, "amazon.nova-pro-v1:0")
}

// TestBedrockAnthropicReasoningMedium verifies that medium reasoning maps to
// reasoningConfig with type "enabled" and budgetTokens 10000 for Claude models.
func TestBedrockAnthropicReasoningMedium(t *testing.T) {
	model := newBedrockClaudeModel()

	level := types.ReasoningMedium
	opts := &provider.GenerateOptions{Reasoning: &level}
	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rc, ok := body["reasoningConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'reasoningConfig', got: %v", body["reasoningConfig"])
	}
	if rc["type"] != "enabled" {
		t.Errorf("expected type 'enabled', got: %v", rc["type"])
	}
	if rc["budgetTokens"] != 10000 {
		t.Errorf("expected budgetTokens 10000, got: %v", rc["budgetTokens"])
	}
}

// TestBedrockAnthropicReasoningNone verifies that ReasoningNone maps to disabled.
func TestBedrockAnthropicReasoningNone(t *testing.T) {
	model := newBedrockClaudeModel()

	level := types.ReasoningNone
	opts := &provider.GenerateOptions{Reasoning: &level}
	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rc, ok := body["reasoningConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'reasoningConfig', got: %v", body["reasoningConfig"])
	}
	if rc["type"] != "disabled" {
		t.Errorf("expected type 'disabled', got: %v", rc["type"])
	}
}

// TestBedrockAnthropicReasoningDefault verifies that ReasoningDefault omits the field.
func TestBedrockAnthropicReasoningDefault(t *testing.T) {
	model := newBedrockClaudeModel()

	level := types.ReasoningDefault
	opts := &provider.GenerateOptions{Reasoning: &level}
	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := body["reasoningConfig"]; ok {
		t.Error("expected no 'reasoningConfig' for provider-default")
	}
}

// TestBedrockAnthropicAllReasoningLevels verifies all budget token mappings.
func TestBedrockAnthropicAllReasoningLevels(t *testing.T) {
	model := newBedrockClaudeModel()

	tests := []struct {
		level       types.ReasoningLevel
		wantType    string
		wantBudget  interface{}
		wantEnabled bool
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
			body, err := model.buildClaudeRequest(opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rc, ok := body["reasoningConfig"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected 'reasoningConfig', got: %v", body["reasoningConfig"])
			}
			if rc["type"] != tt.wantType {
				t.Errorf("type: want %q, got %v", tt.wantType, rc["type"])
			}
			if tt.wantEnabled {
				if rc["budgetTokens"] != tt.wantBudget {
					t.Errorf("budgetTokens: want %v, got %v", tt.wantBudget, rc["budgetTokens"])
				}
			}
		})
	}
}

// TestBedrockNovaReasoningHigh verifies that Nova models use reasoning_effort.
func TestBedrockNovaReasoningHigh(t *testing.T) {
	model := newBedrockNovaModel()

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body, err := model.buildNovaRequest(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if body["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort 'high', got: %v", body["reasoning_effort"])
	}
}

// TestBedrockNovaReasoningLevels verifies the OpenAI-style mapping for Nova.
func TestBedrockNovaReasoningLevels(t *testing.T) {
	model := newBedrockNovaModel()

	tests := []struct {
		level  types.ReasoningLevel
		want   string
		hasKey bool
	}{
		{types.ReasoningNone, "disabled", true},
		{types.ReasoningMinimal, "low", true},
		{types.ReasoningLow, "low", true},
		{types.ReasoningMedium, "medium", true},
		{types.ReasoningHigh, "high", true},
		{types.ReasoningXHigh, "high", true},
		{types.ReasoningDefault, "", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			opts := &provider.GenerateOptions{Reasoning: &level}
			body, err := model.buildNovaRequest(opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			val, hasKey := body["reasoning_effort"]
			if hasKey != tt.hasKey {
				t.Fatalf("reasoning_effort presence: want %v, got %v", tt.hasKey, hasKey)
			}
			if tt.hasKey && val != tt.want {
				t.Errorf("reasoning_effort: want %q, got %v", tt.want, val)
			}
		})
	}
}
