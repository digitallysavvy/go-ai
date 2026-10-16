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
