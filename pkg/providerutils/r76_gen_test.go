package providerutils

import (
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A tool result whose raw value is a Go error is a tool result, not a tool
// error: its model output is createToolModelOutput's json toJSONValue(output)
// ({} for an error without exported fields), and a ModelOutput already set
// (tool ToModelOutput) is kept.
func TestR76GenResponseMessagesErrorValueResult(t *testing.T) {
	empty := &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{}}
	model := &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model"}
	for name, tc := range map[string]struct {
		content []types.ContentPart
		results []types.ToolResult
		want    *types.ToolResultOutput
	}{
		"result":              {results: []types.ToolResult{{ToolCallID: "c1", ToolName: "t", Result: errors.New("boom")}}, want: empty},
		"result model output": {results: []types.ToolResult{{ToolCallID: "c1", ToolName: "t", Result: errors.New("boom"), ModelOutput: model}}, want: model},
		"content":             {content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Result: errors.New("boom")}}, want: empty},
	} {
		msgs := ConvertToResponseMessages(nil, tc.content, tc.results)
		if len(msgs) != 1 || len(msgs[0].Content) != 1 {
			t.Fatalf("%s: messages %#v", name, msgs)
		}
		got, ok := msgs[0].Content[0].(types.ToolResultContent)
		if !ok || !reflect.DeepEqual(got.Output, tc.want) || got.Error != "" {
			t.Fatalf("%s: part %#v, want Output %#v", name, msgs[0].Content[0], tc.want)
		}
	}
}
