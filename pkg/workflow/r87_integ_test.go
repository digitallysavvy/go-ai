package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r87integToolModel struct {
	wfMockModel
	providerExecuted bool
}

func (m *r87integToolModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	m.calls++
	chunks := []provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t1", Arguments: map[string]interface{}{}, ProviderExecuted: m.providerExecuted}}}
	if m.providerExecuted {
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "1", ToolName: "t1", Result: "r", ProviderExecuted: true}})
	}
	chunks = append(chunks,
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "after"},
		provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls})
	return testutil.NewMockTextStream(chunks), nil
}

// StreamText errors after the step's finish-step on a ToModelOutput failure
// (stream ba77a80b, TS toResponseMessages). TS WorkflowAgent converts inside
// its own tool execution (workflow-agent.ts executeToolWithCallbacks /
// resolveProviderToolResult), before writeToolResults writes the results and
// the step boundary, so a WorkflowAgent step ends with the error after the
// model's parts: no tool result for a local call, no finish-step.
func TestR87integWorkflowToModelOutputErrorKeepsWorkflowOrder(t *testing.T) {
	for _, tc := range []struct {
		name             string
		providerExecuted bool
		want             []string
	}{
		{"local", false, []string{"start", "start-step", "tool-call", "text"}},
		{"provider-executed", true, []string{"start", "start-step", "tool-call", "tool-result", "text"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := types.Tool{Name: "t1", ProviderExecuted: tc.providerExecuted,
				ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
					return nil, errors.New("tmo failed")
				}}
			if !tc.providerExecuted {
				tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "r", nil
				}
			}
			model := &r87integToolModel{providerExecuted: tc.providerExecuted}
			agent, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: []types.Tool{tool}})
			if err != nil {
				t.Fatal(err)
			}
			s, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "x"})
			if err != nil {
				t.Fatal(err)
			}
			var parts []string
			for c := range s.Chunks() {
				parts = append(parts, string(c.Type))
			}
			if !reflect.DeepEqual(parts, tc.want) {
				t.Errorf("parts = %v, want %v", parts, tc.want)
			}
			if s.Err() == nil || s.Err().Error() != "tmo failed" {
				t.Errorf("Err() = %v", s.Err())
			}
			if model.calls != 1 {
				t.Errorf("model calls = %d, want 1", model.calls)
			}
		})
	}
}
