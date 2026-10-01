package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Tests ported from packages/openai/src/responses/
// openai-responses-reasoning-effort-update.test.ts (TS row 94d5d6d3e6): the
// request-level providerOptions.openai.reasoningEffortUpdate option is
// validated against the model's supported reasoning efforts, not just
// against whether the model supports configuration updates at all.

// userMsg builds a plain user text message.
func userMsg(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}
}

// TS: 'warns and omits request-level none updates for Astra' (request-level
// only variant; Astra's supported efforts are low/medium/high/xhigh/max,
// excluding 'none' which only gpt-6-sol/luna support).
func TestResponsesReasoningEffortUpdate_RequestLevelRejectsUnsupportedEffort(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{userMsg("Question")}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffort": "medium", "reasoningEffortUpdate": "none"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 1 || warnings[0].Feature != "reasoningEffortUpdate" {
		t.Fatalf("warnings = %#v, want single reasoningEffortUpdate warning", warnings)
	}
	want := "gpt-6-astra only supports the following reasoning efforts: low, medium, high, xhigh, max"
	if warnings[0].Details != want {
		t.Errorf("warning details = %q, want %q", warnings[0].Details, want)
	}
	input, _ := body["input"].([]interface{})
	for _, item := range input {
		if m0, ok := item.(map[string]interface{}); ok && m0["type"] == "configuration_update" {
			t.Fatalf("unexpected configuration_update for a model-unsupported effort: %#v", body["input"])
		}
	}
}

// TS: 'prepends a request-level none update' (gpt-6-sol/luna support 'none').
func TestResponsesReasoningEffortUpdate_RequestLevelAcceptsSupportedEffort(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	for _, modelID := range []string{ModelGPT6Sol, ModelGPT6Luna} {
		t.Run(modelID, func(t *testing.T) {
			m := NewResponsesLanguageModel(p, modelID)
			body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{userMsg("Question")}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"reasoningEffort": "low", "reasoningEffortUpdate": "none"},
				},
			}, false)
			if err != nil {
				t.Fatalf("buildRequest failed: %v", err)
			}
			if len(warnings) != 0 {
				t.Fatalf("warnings = %#v, want none", warnings)
			}
			input, _ := body["input"].([]interface{})
			if len(input) != 2 {
				t.Fatalf("input = %#v, want 2 items", input)
			}
			first, ok := input[0].(map[string]interface{})
			if !ok || first["type"] != "configuration_update" {
				t.Fatalf("input[0] = %#v, want configuration_update", input[0])
			}
			reasoning, _ := first["reasoning"].(map[string]interface{})
			if reasoning["effort"] != "none" {
				t.Fatalf("configuration_update effort = %#v, want none", reasoning)
			}
		})
	}
}
