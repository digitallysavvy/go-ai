package ai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// R75 item 2: with a ModelOutput set, the raw tool result stays in
// ToolResultContent.Result even when it is itself a ToolResultOutput (TS step
// content tool-result `output` is the raw value), in both pkg/ai builders.
func TestR75Gen_ContentKeepsRawToolResultOutputWithModelOutput(t *testing.T) {
	raw := types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "raw"}
	model := &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model"}
	for _, rawValue := range []interface{}{raw, &raw} {
		tr := types.ToolResult{ToolCallID: "c1", ToolName: "t", Result: rawValue, ModelOutput: model}
		parts := toolResultsToContentParts([]types.ToolResult{tr})
		if len(parts) != 1 {
			t.Fatalf("toolResultsToContentParts: %d parts", len(parts))
		}
		for name, p := range map[string]types.ContentPart{"toolResultsToContentParts": parts[0], "toolResultContentFromToolResult": toolResultContentFromToolResult(tr)} {
			got, ok := p.(types.ToolResultContent)
			if !ok {
				t.Fatalf("%s: got %T", name, p)
			}
			if !reflect.DeepEqual(got.Result, rawValue) || got.Output != model {
				t.Fatalf("%s: Result=%#v Output=%#v", name, got.Result, got.Output)
			}
		}
		// Without a model output the ToolResultOutput is the output itself.
		tr.ModelOutput = nil
		for name, p := range map[string]types.ContentPart{"toolResultsToContentParts": toolResultsToContentParts([]types.ToolResult{tr})[0], "toolResultContentFromToolResult": toolResultContentFromToolResult(tr)} {
			got := p.(types.ToolResultContent)
			if got.Result != nil || got.Output == nil || got.Output.Value != "raw" {
				t.Fatalf("%s without model output: Result=%#v Output=%#v", name, got.Result, got.Output)
			}
		}
	}
}
