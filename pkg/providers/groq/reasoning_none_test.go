package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported (behaviorally) from ai/packages/groq/src/groq-chat-language-model.ts's
// reasoning: 'none' handling (9e9bf11) and serviceTier option (7bf717f).

func TestGroq_ReasoningNone_SupportedModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewLanguageModel(p, "qwen/qwen3.6-27b")

	none := types.ReasoningNone
	body, warnings := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &none,
	}, false)

	if body["reasoning_effort"] != "none" {
		t.Fatalf("reasoning_effort = %#v, want none", body["reasoning_effort"])
	}
	for _, w := range warnings {
		if w.Feature == "reasoning" {
			t.Fatalf("did not expect a reasoning warning for the supported model, got %+v", warnings)
		}
	}
}

func TestGroq_ReasoningNone_UnsupportedModelWarns(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewLanguageModel(p, "llama-3.3-70b-versatile")

	none := types.ReasoningNone
	body, warnings := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &none,
	}, false)

	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("did not expect reasoning_effort to be set, got %#v", body["reasoning_effort"])
	}
	found := false
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "reasoning" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an unsupported reasoning warning, got %+v", warnings)
	}
}

func TestGroq_ExplicitReasoningEffortOverridesTopLevelNone(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewLanguageModel(p, "llama-3.3-70b-versatile")

	none := types.ReasoningNone
	body, warnings := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &none,
		ProviderOptions: map[string]interface{}{
			"groq": map[string]interface{}{"reasoningEffort": "high"},
		},
	}, false)

	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", body["reasoning_effort"])
	}
	for _, w := range warnings {
		if w.Feature == "reasoning" {
			t.Fatalf("did not expect a reasoning warning when reasoningEffort is explicit, got %+v", warnings)
		}
	}
}

func TestGroq_ServiceTierForwarded(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewLanguageModel(p, "llama-3.3-70b-versatile")

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"groq": map[string]interface{}{"serviceTier": "flex"},
		},
	}, false)

	if body["service_tier"] != "flex" {
		t.Fatalf("service_tier = %#v, want flex", body["service_tier"])
	}
}

// TestGroq_ParallelToolCallsAndReasoningFormatForwarded guards two Groq chat
// options TS always sets when present (groq-chat-language-model.ts:212,240)
// that the Go port previously never read under any key:
// providerOptions.groq.parallelToolCalls -> parallel_tool_calls and
// providerOptions.groq.reasoningFormat -> reasoning_format.
func TestGroq_ParallelToolCallsAndReasoningFormatForwarded(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewLanguageModel(p, "llama-3.3-70b-versatile")

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"groq": map[string]interface{}{
				"parallelToolCalls": false,
				"reasoningFormat":   "hidden",
			},
		},
	}, false)

	if body["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %#v, want false", body["parallel_tool_calls"])
	}
	if body["reasoning_format"] != "hidden" {
		t.Fatalf("reasoning_format = %#v, want hidden", body["reasoning_format"])
	}
}
