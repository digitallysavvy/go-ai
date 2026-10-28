package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseToolCall throws NoSuchToolError without repair only for
// `tools == null`. A tool set that ActiveTools filters down to {} goes
// through doParseToolCall: the error lists the empty available tools
// ("Available tools: .") and RepairToolCall runs.
func TestR53ParseToolCallEmptyActiveToolsRepairs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tools       []types.Tool
		active      []string
		wantMsg     string
		wantRepairs int
	}{
		{"no tools", nil, nil, "Model tried to call unavailable tool 'testTool'. No tools are available.", 0},
		{"all filtered out", []types.Tool{pipelineTestTool("testTool", nil)}, []string{}, "Model tried to call unavailable tool 'testTool'. Available tools: .", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repairs := 0
			result, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:       toolCallModel([]types.ToolCall{{ID: "c1", ToolName: "testTool", RawArguments: `{}`}}, ""),
				Prompt:      "hi",
				Tools:       tc.tools,
				ActiveTools: tc.active,
				StopWhen:    []StopCondition{IsStepCount(1)},
				RepairToolCall: func(context.Context, ToolCallRepairOptions) (*types.ToolCall, error) {
					repairs++
					return nil, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			call := result.Steps[0].ToolCalls[0]
			if !call.Invalid || call.Error == nil || call.Error.Error() != tc.wantMsg {
				t.Fatalf("call = %+v, want invalid with %q", call, tc.wantMsg)
			}
			if repairs != tc.wantRepairs {
				t.Fatalf("repairs = %d, want %d", repairs, tc.wantRepairs)
			}
		})
	}
}
