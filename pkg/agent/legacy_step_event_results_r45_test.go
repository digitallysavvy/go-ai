package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The legacy loop's step-end event reported the raw tool results, approval
// requests included, while its Static/DynamicToolResults and the step result
// hold only real results (as GenerateText's step event does).
func TestGenerateAgentStepEndEventToolResultsArePublic(t *testing.T) {
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "t1", Arguments: map[string]interface{}{}}},
	}}}
	var got []types.ToolResult
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{Name: "t1", Parameters: map[string]interface{}{"type": "object"}, ToolApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}}},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{
		Prompt:    "go",
		OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) { got = e.ToolResults },
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("step event ToolResults = %+v, want none (the call awaits approval)", got)
	}
}
