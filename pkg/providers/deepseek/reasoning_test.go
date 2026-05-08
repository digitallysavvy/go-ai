package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DeepSeek uses { thinking: { type: "enabled" | "disabled" } }, not reasoning_effort.

func TestDeepSeekReasoningAllLevels(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	tests := []struct {
		level       types.ReasoningLevel
		wantType    string
		hasThinking bool
	}{
		{types.ReasoningNone, "disabled", true},
		{types.ReasoningMinimal, "enabled", true},
		{types.ReasoningLow, "enabled", true},
		{types.ReasoningMedium, "enabled", true},
		{types.ReasoningHigh, "enabled", true},
		{types.ReasoningXHigh, "enabled", true},
		{types.ReasoningDefault, "", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			opts := &provider.GenerateOptions{Reasoning: &level}
			body := model.buildRequestBody(opts, false)

			thinkingRaw, hasKey := body["thinking"]
			if hasKey != tt.hasThinking {
				t.Fatalf("thinking presence: want %v, got %v", tt.hasThinking, hasKey)
			}
			if tt.hasThinking {
				thinking, ok := thinkingRaw.(map[string]interface{})
				if !ok {
					t.Fatalf("thinking should be map[string]interface{}, got %T", thinkingRaw)
				}
				if thinking["type"] != tt.wantType {
					t.Errorf("thinking.type: want %q, got %v", tt.wantType, thinking["type"])
				}
			}
			if _, ok := body["reasoning_effort"]; ok {
				t.Error("DeepSeek should not set reasoning_effort; it uses thinking object")
			}
		})
	}
}

func TestDeepSeekReasoningNilOmitted(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-chat")

	opts := &provider.GenerateOptions{}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["thinking"]; ok {
		t.Error("expected no thinking when Reasoning is nil")
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Error("expected no reasoning_effort when Reasoning is nil")
	}
}

func TestDeepSeekReasoningPropagated(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	thinkingRaw, ok := body["thinking"]
	if !ok {
		t.Fatal("expected thinking to be set for ReasoningHigh")
	}
	thinking, ok := thinkingRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("thinking should be map[string]interface{}, got %T", thinkingRaw)
	}
	if thinking["type"] != "enabled" {
		t.Errorf("expected thinking.type 'enabled', got: %v", thinking["type"])
	}
}

func TestDeepSeekProviderOptionsOverrideReasoning(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"deepseek": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "disabled"},
			},
		},
	}
	body, warnings := model.buildRequestBodyWithWarnings(opts, false)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for camelCase provider options, got %#v", warnings)
	}
	thinking := body["thinking"].(map[string]interface{})
	if thinking["type"] != "disabled" {
		t.Errorf("provider options should override top-level Reasoning; got thinking.type=%v", thinking["type"])
	}
}
