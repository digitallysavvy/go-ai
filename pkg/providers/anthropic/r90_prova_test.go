package anthropic

import (
	"context"
	"strings"
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

// TS doGenerate tool_use: input is JSON.stringify(part.input) (z.unknown()):
// a null input is "null", a non-object its JSON text (TS recorded at
// 9f7a842ae1).
func TestR90provaToolUseNonObjectInput(t *testing.T) {
	s := r88provaNewServer(t, `{"type":"message","id":"m","model":"x","content":[{"type":"tool_use","id":"c1","name":"t","input":null},{"type":"tool_use","id":"c3","name":"t","input":[1]}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, nil)
	res, err := r88provaModel(t, s, "claude-sonnet-4-6").DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{{Type: types.ToolTypeFunction, Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tc := range res.ToolCalls {
		got = append(got, tc.RawArguments)
	}
	if strings.Join(got, "|") != "null|[1]" {
		t.Errorf("inputs = %q", got)
	}
}

// Same rule for server_tool_use and mcp_tool_use (TS recorded at
// 9f7a842ae1): web_search null -> "null", the code execution sub-tools
// spread null -> {"type":...}, mcp null -> "null"; a streamed
// message_start tool_use uses part.input ?? {} -> "{}".
func TestR90provaServerToolUseNullInput(t *testing.T) {
	s := r88provaNewServer(t, `{"type":"message","id":"m","model":"x","content":[{"type":"server_tool_use","id":"s1","name":"web_search","input":null},{"type":"server_tool_use","id":"s2","name":"bash_code_execution","input":null},{"type":"mcp_tool_use","id":"m1","name":"mm","input":null,"server_name":"srv"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
		[]string{
			`{"type":"message_start","message":{"id":"m","model":"x","usage":{"input_tokens":1},"content":[{"type":"tool_use","id":"p1","name":"t","input":null}]}}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`,
		})
	m := r88provaModel(t, s, "claude-sonnet-4-6")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{{Type: types.ToolTypeProviderDefined, Name: "web_search", ProviderID: "anthropic.web_search_20250305", ProviderArgs: map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tc := range res.ToolCalls {
		got = append(got, tc.ID+"="+tc.RawArguments)
	}
	if want := `s1=null|s2={"type":"bash_code_execution"}|m1=null`; strings.Join(got, "|") != want {
		t.Errorf("inputs = %s, want %s", strings.Join(got, "|"), want)
	}
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	parts, err := r88provaDrain(stream)
	if err != nil {
		t.Fatal(err)
	}
	var streamed []string
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolCall {
			streamed = append(streamed, p.ToolCall.RawArguments)
		}
	}
	if strings.Join(streamed, "|") != "{}" {
		t.Errorf("streamed inputs = %q, want [{}]", streamed)
	}
}
