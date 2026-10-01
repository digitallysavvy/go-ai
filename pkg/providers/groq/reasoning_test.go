package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func makeTestGroqProvider() *Provider {
	return New(Config{APIKey: "test-key"})
}

func TestGroqReasoningAllLevels(t *testing.T) {
	prov := makeTestGroqProvider()
	model := NewLanguageModel(prov, "deepseek-r1-distill-llama-70b")

	tests := []struct {
		level  types.ReasoningLevel
		want   string
		hasKey bool
	}{
		// ReasoningNone: Groq does not accept "disabled"; omit the field.
		{types.ReasoningNone, "", false},
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

func TestGroqReasoningNilOmitted(t *testing.T) {
	prov := makeTestGroqProvider()
	model := NewLanguageModel(prov, "llama-3.3-70b-versatile")

	opts := &provider.GenerateOptions{}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["reasoning_effort"]; ok {
		t.Error("expected no reasoning_effort when Reasoning is nil")
	}
}

func TestGroqReasoningPropagated(t *testing.T) {
	prov := makeTestGroqProvider()
	model := NewLanguageModel(prov, "deepseek-r1-distill-llama-70b")

	level := types.ReasoningLow
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if body["reasoning_effort"] != "low" {
		t.Errorf("expected reasoning_effort 'low', got: %v", body["reasoning_effort"])
	}
}

func TestGroqCamelCaseProviderOptionsOverrideReasoning(t *testing.T) {
	prov := makeTestGroqProvider()
	model := NewLanguageModel(prov, "deepseek-r1-distill-llama-70b")

	level := types.ReasoningLow
	opts := &provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"groq": map[string]interface{}{
				"reasoningEffort": "high",
				"textVerbosity":   "low",
				"user":            "user-123",
			},
		},
	}
	body, warnings := model.buildRequestBodyWithWarnings(opts, false)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for camelCase provider options, got %#v", warnings)
	}
	if body["reasoning_effort"] != "high" {
		t.Errorf("expected provider reasoningEffort to override reasoning, got %v", body["reasoning_effort"])
	}
	if body["verbosity"] != "low" {
		t.Errorf("expected textVerbosity to serialize as verbosity, got %v", body["verbosity"])
	}
	if body["user"] != "user-123" {
		t.Errorf("expected user option, got %v", body["user"])
	}
}

func TestGroqDeprecatedOpenAICompatibleProviderOptionsWarning(t *testing.T) {
	prov := makeTestGroqProvider()
	model := NewLanguageModel(prov, "deepseek-r1-distill-llama-70b")

	opts := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"openai-compatible": map[string]interface{}{
				"reasoning-effort": "medium",
			},
		},
	}
	body, warnings := model.buildRequestBodyWithWarnings(opts, false)

	if body["reasoning_effort"] != "medium" {
		t.Errorf("expected deprecated reasoning-effort to be accepted, got %v", body["reasoning_effort"])
	}
	if len(warnings) != 2 {
		t.Fatalf("expected two deprecation warnings, got %#v", warnings)
	}
	if warnings[0].Type != "deprecated" || warnings[0].Feature != "providerOptions key 'openai-compatible'" {
		t.Fatalf("unexpected warning: %#v", warnings[0])
	}
	if warnings[1].Type != "deprecated" || warnings[1].Feature != "provider option key 'reasoning-effort'" {
		t.Fatalf("unexpected option warning: %#v", warnings[1])
	}
}
