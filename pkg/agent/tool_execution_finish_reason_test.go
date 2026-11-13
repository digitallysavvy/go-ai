package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS generate-text.ts runs local tools only when the step finished with
// stop or tool-calls (isToolExecutionAllowedFinishReason): a truncated,
// filtered or errored step ends the run without executing the call.
func TestToolLoopAgent_NonStopFinishReasonSkipsLocalToolExecution(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason types.FinishReason
		runs   int32
	}{
		{"length", types.FinishReasonLength, 0},
		{"content-filter", types.FinishReasonContentFilter, 0},
		{"error", types.FinishReasonError, 0},
		{"tool-calls", types.FinishReasonToolCalls, 1},
		{"stop", types.FinishReasonStop, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var executed int32
			tool := types.Tool{
				Name:       "lookup",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					atomic.AddInt32(&executed, 1)
					return "ok", nil
				},
			}
			model := &mockLanguageModel{responses: []types.GenerateResult{
				{
					FinishReason: tc.reason,
					ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "lookup", Arguments: map[string]interface{}{}}},
				},
				{Text: "done", FinishReason: types.FinishReasonStop},
			}}
			a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, StopWhen: []ai.StopCondition{ai.IsStepCount(2)}})
			// GenerateAgent runs the agent's own loop (runLoop); Generate
			// delegates to ai.GenerateText, which has its own check.
			if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
				t.Fatal(err)
			}
			if got := atomic.LoadInt32(&executed); got != tc.runs {
				t.Fatalf("tool executed %d times, want %d", got, tc.runs)
			}
		})
	}
}
