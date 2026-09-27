package fireworks

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFireworksReasoningAllLevels(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/deepseek-r1")

	tests := []struct {
		level  types.ReasoningLevel
		want   string
		hasKey bool
	}{
		// TS: reasoning_effort is set to the raw reasoning value whenever
		// isCustomReasoning(reasoning) is true, which excludes only
		// undefined/'provider-default' — NOT 'none'. Fireworks' own
		// transformRequestBody only remaps minimal->low and xhigh->high,
		// passing "none" through unchanged.
		{types.ReasoningNone, "none", true},
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
			body := model.buildRequestBody(opts, false)

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

func TestFireworksReasoningNilOmitted(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/deepseek-r1")

	opts := &provider.GenerateOptions{}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["reasoning_effort"]; ok {
		t.Error("expected no reasoning_effort when Reasoning is nil")
	}
}

func TestFireworksReasoningEffortPropagated(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/deepseek-r1")

	level := types.ReasoningMedium
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if body["reasoning_effort"] != "medium" {
		t.Errorf("expected reasoning_effort 'medium', got: %v", body["reasoning_effort"])
	}
}
