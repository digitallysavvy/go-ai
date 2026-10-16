package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertFunctionToolResultOutput: unsupported content parts are dropped
// with an `other` warning, and an all-dropped output is sent as [].
func TestConvert_UnsupportedToolContentPartWarns(t *testing.T) {
	tool := types.Message{Role: types.RoleTool, Content: []types.ContentPart{
		types.ToolResultContent{ToolCallID: "c1", ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.CustomContentBlock{},
		}}},
	}}
	input, warnings, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{tool}}, "system", ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].Type != "other" || warnings[0].Message != "unsupported tool content part type: custom" {
		t.Fatalf("warnings = %+v", warnings)
	}
	got := mustJSON(input)
	if got != `[{"type":"function_call_output","call_id":"c1","output":[]}]` {
		t.Fatalf("input = %s", got)
	}
}

// TS function_call_output carries the result's providerOptions caller.
func TestConvert_FunctionCallOutputCarriesCaller(t *testing.T) {
	out := func(opts map[string]interface{}) string {
		tool := types.Message{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "c1", ToolName: "f", ProviderOptions: opts,
				Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"}},
		}}
		input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{tool}}, "system", ConvertOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return mustJSON(input)
	}
	got := out(map[string]interface{}{"openai": map[string]interface{}{"caller": map[string]interface{}{"type": "program", "callerId": "p1"}}})
	if got != `[{"type":"function_call_output","call_id":"c1","output":"ok","caller":{"type":"program","caller_id":"p1"}}]` {
		t.Fatalf("program caller: %s", got)
	}
	got = out(map[string]interface{}{"openai": map[string]interface{}{"caller": map[string]interface{}{"type": "direct"}}})
	if got != `[{"type":"function_call_output","call_id":"c1","output":"ok","caller":{"type":"direct"}}]` {
		t.Fatalf("direct caller: %s", got)
	}
	if got = out(nil); got != `[{"type":"function_call_output","call_id":"c1","output":"ok"}]` {
		t.Fatalf("no caller: %s", got)
	}
}

// TS custom tool outputs warn "unsupported custom tool content part type".
func TestConvert_CustomToolContentWarningText(t *testing.T) {
	tool := types.Message{Role: types.RoleTool, Content: []types.ContentPart{
		types.ToolResultContent{ToolCallID: "c1", ToolName: "ct", Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.CustomContentBlock{},
		}}},
	}}
	_, warnings, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{tool}}, "system", ConvertOptions{CustomToolNames: map[string]bool{"ct": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].Message != "unsupported custom tool content part type: custom" {
		t.Fatalf("warnings = %+v", warnings)
	}
}
