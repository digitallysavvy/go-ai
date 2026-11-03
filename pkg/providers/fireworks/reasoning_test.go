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
		// TS 61a315e333: Fireworks accepts every reasoning effort for most
		// models, so each level is sent as is.
		{types.ReasoningNone, "none", true},
		{types.ReasoningMinimal, "minimal", true},
		{types.ReasoningLow, "low", true},
		{types.ReasoningMedium, "medium", true},
		{types.ReasoningHigh, "high", true},
		{types.ReasoningXHigh, "xhigh", true},
		{types.ReasoningMax, "max", true},
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

// TestFireworksReasoningEffortProviderOptionOverride ports TS
// (openai-compatible-chat-language-model.ts:310-312): an explicit
// providerOptions.fireworks.reasoningEffort always wins over the unified
// Reasoning field ("compatibleOptions.reasoningEffort ?? reasoning").
func TestFireworksReasoningEffortProviderOptionOverride(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/deepseek-r1")

	level := types.ReasoningLow
	opts := &provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"fireworks": map[string]interface{}{
				"reasoningEffort": "high",
			},
		},
	}
	body := model.buildRequestBody(opts, false)

	if body["reasoning_effort"] != "high" {
		t.Errorf("expected providerOptions.fireworks.reasoningEffort to win: want 'high', got %v", body["reasoning_effort"])
	}
}

// TestFireworksReasoningEffortProviderOptionOverrideRemapped: TS 61a315e333
// no longer rewrites providerOptions.fireworks.reasoningEffort; it is sent
// unchanged, and an explicit null is sent as null (5006d83c4e).
func TestFireworksReasoningEffortProviderOptionOverrideRemapped(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/nemotron-3-ultra-nvfp4")

	for _, effort := range []interface{}{"minimal", "max", nil} {
		low := types.ReasoningLow
		body, warnings := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Reasoning: &low,
			ProviderOptions: map[string]interface{}{
				"fireworks": map[string]interface{}{"reasoningEffort": effort},
			},
		}, false)
		v, has := body["reasoning_effort"]
		if !has || v != effort || len(warnings) != 0 {
			t.Errorf("override %v: reasoning_effort = %#v (present %v), warnings %#v", effort, v, has, warnings)
		}
	}
}

// TS 61a315e333 getFireworksReasoningEffortMap: "none" rounds up to
// "minimal" for gpt-oss / glm-5p3 / muse-spark, "minimal" rounds up to
// "low" for nemotron / inkling, each with a compatibility warning.
func TestFireworksReasoningCoercionWarnings(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	for _, tt := range []struct {
		model string
		level types.ReasoningLevel
		want  string
	}{
		{"accounts/fireworks/models/gpt-oss-120b", types.ReasoningNone, "minimal"},
		{"accounts/fireworks/models/glm-5p3", types.ReasoningNone, "minimal"},
		{"accounts/fireworks/models/muse-spark", types.ReasoningNone, "minimal"},
		{"accounts/fireworks/models/nemotron-3-ultra-nvfp4", types.ReasoningMinimal, "low"},
		{"inkling-1", types.ReasoningMinimal, "low"},
	} {
		l := tt.level
		body, warnings := NewLanguageModel(p, tt.model).buildRequestBodyWithWarnings(&provider.GenerateOptions{Reasoning: &l}, true)
		if body["reasoning_effort"] != tt.want || len(warnings) != 1 || warnings[0].Type != "compatibility" || warnings[0].Feature != "reasoning" ||
			warnings[0].Details != `reasoning "`+string(tt.level)+`" is not directly supported by this model. mapped to effort "`+tt.want+`".` {
			t.Errorf("%s %s: reasoning_effort %v, warnings = %#v", tt.model, tt.level, body["reasoning_effort"], warnings)
		}
	}
	h := types.ReasoningMax
	if _, warnings := NewLanguageModel(p, "accounts/fireworks/models/deepseek-r1").buildRequestBodyWithWarnings(&provider.GenerateOptions{Reasoning: &h}, false); len(warnings) != 0 {
		t.Fatalf("unexpected warnings %#v", warnings)
	}
}
