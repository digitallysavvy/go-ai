package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 84208104a4 anthropic-language-model.test.ts "should return correct
// capabilities for claude-fable-5-2": the catalog treats Fable 5.2 like 5.1.
func TestR90provaFable52Capabilities(t *testing.T) {
	want := ModelCapabilities{
		MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
		RejectsSamplingParameters: true, SupportsXHighEffort: true,
		RejectsThinkingDisabled: true, RejectsBudgetThinking: true, RejectsForcedToolUse: true, IsKnownModel: true,
	}
	if got := GetModelCapabilities(ClaudeFable5_2); got != want {
		t.Fatalf("GetModelCapabilities(%q) = %#v, want %#v", ClaudeFable5_2, got, want)
	}
}

// TS 84208104a4: "should use auto tool choice for %s when tool choice is
// "required"" (claude-fable-5-1, claude-fable-5-2).
func TestR90provaFableRequiredToolChoiceIsAuto(t *testing.T) {
	for _, id := range []string{ClaudeFable5_1, ClaudeFable5_2} {
		m := NewLanguageModel(New(Config{APIKey: "k"}), id, nil)
		req, err := m.prepareRequest(&provider.GenerateOptions{
			Prompt:     types.Prompt{Text: "Hello"},
			Tools:      []types.Tool{{Type: types.ToolTypeFunction, Name: "test-tool", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}},
			ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
		}, false)
		if err != nil {
			t.Fatal(err)
		}
		tc, _ := req.body["tool_choice"].(map[string]interface{})
		if tc["type"] != "auto" || len(req.warnings) != 1 {
			t.Errorf("%s: tool_choice = %v, warnings = %v", id, req.body["tool_choice"], req.warnings)
		}
	}
}

// TS 84208104a4: "should remove disabled thinking for claude-fable-5-2 and
// warn".
func TestR90provaFable52RemovesDisabledThinking(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), ClaudeFable5_2, nil)
	req, err := m.prepareRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{
			"thinking": map[string]interface{}{"type": "disabled"},
			"effort":   "xhigh",
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := req.body["thinking"]; ok {
		t.Errorf("thinking = %v, want absent", req.body["thinking"])
	}
	if req.body["max_tokens"] != 128000 {
		t.Errorf("max_tokens = %v, want 128000", req.body["max_tokens"])
	}
	oc, _ := req.body["output_config"].(map[string]interface{})
	if len(oc) != 1 || oc["effort"] != "xhigh" {
		t.Errorf("output_config = %v", req.body["output_config"])
	}
	if len(req.warnings) == 0 || req.warnings[0].Feature != "providerOptions.anthropic.thinking" {
		t.Errorf("warnings = %v", req.warnings)
	}
}

// TS 84208104a4 anthropic-forced-tool-structured-output.test.ts: native
// structured output for claude-fable-5-2 when the provider disables it.
func TestR90provaFable52ForcedToolStructuredOutput(t *testing.T) {
	body, _ := r85provaBody(t, r85provaForcedModel(ClaudeFable5_2, false), r85provaJSONCall(r85provaSchema), false)
	oc, _ := body["output_config"].(map[string]interface{})
	format, _ := oc["format"].(map[string]interface{})
	if format["type"] != "json_schema" || format["schema"] == nil || body["tools"] != nil || body["tool_choice"] != nil {
		t.Errorf("body = %v", body)
	}
}
