package agent

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// R75 item 2: the legacy builder agrees with ai.toolResultsToContentParts. A
// raw result that is itself a ToolResultOutput stays in Result when a
// ModelOutput is set (TS content `output` is the raw value), and moves to
// Output only without one. IsError is carried as in pkg/ai.
func TestR75GenAgentContentKeepsRawToolResultOutputWithModelOutput(t *testing.T) {
	raw := types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "raw"}
	model := &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model"}
	isErr := true
	for _, rawValue := range []interface{}{raw, &raw} {
		tr := types.ToolResult{ToolCallID: "c1", ToolName: "t", Result: rawValue, ModelOutput: model, IsError: &isErr}
		parts := agentToolResultsToContentParts([]types.ToolResult{tr}, nil)
		if len(parts) != 1 {
			t.Fatalf("%d parts", len(parts))
		}
		got := parts[0].(types.ToolResultContent)
		if !reflect.DeepEqual(got.Result, rawValue) || got.Output == nil || got.Output.Value != "model" {
			t.Fatalf("with model output: Result=%#v Output=%#v", got.Result, got.Output)
		}
		if got.IsError == nil || !*got.IsError {
			t.Fatalf("IsError not carried: %v", got.IsError)
		}
		tr.ModelOutput = nil
		got = agentToolResultsToContentParts([]types.ToolResult{tr}, nil)[0].(types.ToolResultContent)
		if got.Result != nil || got.Output == nil || got.Output.Value != "raw" {
			t.Fatalf("without model output: Result=%#v Output=%#v", got.Result, got.Output)
		}
	}
}
