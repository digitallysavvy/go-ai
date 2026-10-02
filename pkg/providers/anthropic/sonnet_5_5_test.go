package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS's "claude-sonnet-5-5 specific behavior" describe block
// (anthropic-language-model.test.ts, commit e361d39285, #21631):
// claude-sonnet-5-5 rejects thinking.type "disabled" and budget-based
// thinking, forced tool_choice, and the older computer_* tool types, and
// adds thinking.type "between_tools", its lowest thinking setting.

func TestSonnet5_5_NoThinkingByDefault(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, ClaudeSonnet5_5, nil)

	req, err := m.prepareRequest(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	if _, ok := req.body["thinking"]; ok {
		t.Fatalf("thinking = %#v, want omitted", req.body["thinking"])
	}
	if req.body["max_tokens"] != 128000 {
		t.Fatalf("max_tokens = %#v, want 128000", req.body["max_tokens"])
	}
	if len(req.warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", req.warnings)
	}
}

func TestSonnet5_5_SendsBetweenToolsThinking(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, ClaudeSonnet5_5, nil)

	req, err := m.prepareRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "between_tools"},
				"effort":   "medium",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	thinking, _ := req.body["thinking"].(map[string]interface{})
	if thinking["type"] != "between_tools" {
		t.Fatalf("thinking = %#v, want type between_tools", thinking)
	}
	outputConfig, _ := req.body["output_config"].(map[string]interface{})
	if outputConfig["effort"] != "medium" {
		t.Fatalf("output_config = %#v, want effort medium", outputConfig)
	}
	if len(req.warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", req.warnings)
	}
}

func TestSonnet5_5_ReplacesDisabledThinkingWithBetweenToolsAndWarns(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, ClaudeSonnet5_5, nil)

	req, err := m.prepareRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "disabled"},
				"effort":   "low",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	thinking, _ := req.body["thinking"].(map[string]interface{})
	if thinking["type"] != "between_tools" {
		t.Fatalf("thinking = %#v, want type between_tools", thinking)
	}
	outputConfig, _ := req.body["output_config"].(map[string]interface{})
	if outputConfig["effort"] != "low" {
		t.Fatalf("output_config = %#v, want effort low", outputConfig)
	}
	want := types.Warning{
		Type:    "unsupported",
		Feature: "providerOptions.anthropic.thinking",
		Details: "thinking cannot be disabled for claude-sonnet-5-5. Using 'between_tools' thinking, the lowest thinking setting, instead.",
	}
	if len(req.warnings) != 1 || req.warnings[0] != want {
		t.Fatalf("warnings = %#v, want %#v", req.warnings, want)
	}
}

func TestSonnet5_5_LowersXhighEffortToHighWithBetweenToolsThinking(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, ClaudeSonnet5_5, nil)

	req, err := m.prepareRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "between_tools"},
				"effort":   "xhigh",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	outputConfig, _ := req.body["output_config"].(map[string]interface{})
	if outputConfig["effort"] != "high" {
		t.Fatalf("output_config = %#v, want effort lowered to high", outputConfig)
	}
	want := types.Warning{
		Type:    "unsupported",
		Feature: "providerOptions.anthropic.effort",
		Details: "effort 'xhigh' is not supported with 'between_tools' thinking. The effort has been lowered to 'high'.",
	}
	if len(req.warnings) != 1 || req.warnings[0] != want {
		t.Fatalf("warnings = %#v, want %#v", req.warnings, want)
	}
}

func TestSonnet5_5_ReasoningNoneMapsToBetweenTools(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, ClaudeSonnet5_5, nil)

	reasoning := types.ReasoningNone
	req, err := m.prepareRequest(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &reasoning,
	}, false)
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	thinking, _ := req.body["thinking"].(map[string]interface{})
	if thinking["type"] != "between_tools" {
		t.Fatalf("thinking = %#v, want type between_tools", thinking)
	}
}
