package alibaba

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestAlibabaReasoningNoneDisablesThinking(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "qwen-plus")

	level := types.ReasoningNone
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if body["enable_thinking"] != false {
		t.Errorf("expected enable_thinking=false for ReasoningNone, got: %v", body["enable_thinking"])
	}
	if _, ok := body["thinking_budget"]; ok {
		t.Error("expected no thinking_budget for ReasoningNone")
	}
}

func TestAlibabaReasoningEnablesThinkingWithBudget(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "qwen-plus")

	// Budgets derived from mapReasoningToProviderBudget(maxOutputTokens=16384, maxReasoningBudget=16384):
	//   minimal: max(1024, round(16384*0.02))=max(1024,328)=1024
	//   low:     max(1024, round(16384*0.10))=max(1024,1638)=1638
	//   medium:  max(1024, round(16384*0.30))=max(1024,4915)=4915
	//   high:    max(1024, round(16384*0.60))=max(1024,9830)=9830
	//   xhigh:   max(1024, round(16384*0.90))=max(1024,14746)=14746
	tests := []struct {
		level      types.ReasoningLevel
		wantBudget int
	}{
		{types.ReasoningMinimal, 1024},
		{types.ReasoningLow, 1638},
		{types.ReasoningMedium, 4915},
		{types.ReasoningHigh, 9830},
		{types.ReasoningXHigh, 14746},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			opts := &provider.GenerateOptions{Reasoning: &level}
			body := model.buildRequestBody(opts, false)

			if body["enable_thinking"] != true {
				t.Errorf("expected enable_thinking=true for %s, got: %v", tt.level, body["enable_thinking"])
			}
			if body["thinking_budget"] != tt.wantBudget {
				t.Errorf("expected thinking_budget=%d for %s, got: %v", tt.wantBudget, tt.level, body["thinking_budget"])
			}
		})
	}
}

func TestAlibabaReasoningDefaultOmitted(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "qwen-plus")

	level := types.ReasoningDefault
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["enable_thinking"]; ok {
		t.Error("expected no enable_thinking for ReasoningDefault")
	}
	if _, ok := body["thinking_budget"]; ok {
		t.Error("expected no thinking_budget for ReasoningDefault")
	}
}

func TestAlibabaReasoningNilOmitted(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "qwen-plus")

	opts := &provider.GenerateOptions{}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["enable_thinking"]; ok {
		t.Error("expected no enable_thinking when Reasoning is nil")
	}
}

func TestAlibabaReasoningPropagated(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "qwen-plus")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if body["enable_thinking"] != true {
		t.Errorf("expected enable_thinking=true for ReasoningHigh")
	}
	if body["thinking_budget"] != 9830 {
		t.Errorf("expected thinking_budget=9830 for ReasoningHigh, got: %v", body["thinking_budget"])
	}
}

func TestAlibabaProviderOptionsOverrideReasoning(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "qwen-plus")

	// Top-level says High (enable_thinking=true, budget=9830),
	// but provider options override to disable thinking.
	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{
				"enable_thinking": false,
			},
		},
	}
	body := model.buildRequestBody(opts, false)

	if body["enable_thinking"] != false {
		t.Errorf("provider options should override top-level Reasoning; expected enable_thinking=false")
	}
}

func TestAlibabaCamelCaseProviderOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "qwen-plus")

	opts := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{
				"enableThinking":    true,
				"thinkingBudget":    4096,
				"parallelToolCalls": false,
			},
		},
	}
	body, warnings := model.buildRequestBodyWithWarnings(opts, false)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for camelCase provider options, got %#v", warnings)
	}
	if body["enable_thinking"] != true {
		t.Errorf("expected enableThinking to serialize as enable_thinking=true, got %v", body["enable_thinking"])
	}
	if body["thinking_budget"] != 4096 {
		t.Errorf("expected thinkingBudget to serialize as thinking_budget=4096, got %v", body["thinking_budget"])
	}
	if body["parallel_tool_calls"] != false {
		t.Errorf("expected parallelToolCalls to serialize as parallel_tool_calls=false, got %v", body["parallel_tool_calls"])
	}
}
