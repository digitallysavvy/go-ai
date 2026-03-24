package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func makeTestOpenAIProvider() *Provider {
	return New(Config{APIKey: "test-key"})
}

func TestOpenAIReasoningHigh(t *testing.T) {
	prov := makeTestOpenAIProvider()
	model := NewLanguageModel(prov, "o3")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if body["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort 'high', got: %v", body["reasoning_effort"])
	}
}

func TestOpenAIReasoningXHighMapsToHigh(t *testing.T) {
	prov := makeTestOpenAIProvider()
	model := NewLanguageModel(prov, "o3")

	level := types.ReasoningXHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if body["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort 'high' for xhigh, got: %v", body["reasoning_effort"])
	}
}

func TestOpenAIReasoningAllLevels(t *testing.T) {
	prov := makeTestOpenAIProvider()
	model := NewLanguageModel(prov, "o3")

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

func TestOpenAIReasoningNilOmitted(t *testing.T) {
	prov := makeTestOpenAIProvider()
	model := NewLanguageModel(prov, "gpt-4o")

	opts := &provider.GenerateOptions{}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["reasoning_effort"]; ok {
		t.Error("expected no reasoning_effort when Reasoning is nil")
	}
}
