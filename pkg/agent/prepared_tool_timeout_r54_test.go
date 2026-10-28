package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A PrepareCall timeout (TS `timeout ?? preparedCall.timeout`) bounds the
// step's tool executions in the legacy loop, as it bounds the step.
func TestGenerateAgentToolTimeoutFromPrepareCall(t *testing.T) {
	toolMs := 20 * time.Millisecond
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "slow", Arguments: map[string]interface{}{}}},
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	slow := types.Tool{Name: "slow", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return "late", nil
			}
		}}
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{slow},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Timeout = &ai.TimeoutConfig{ToolMs: &toolMs}
			return c
		},
	})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var timeoutErr *ai.TimeoutError
	if len(res.ToolResults) != 1 || !errors.As(res.ToolResults[0].Error, &timeoutErr) {
		t.Fatalf("tool results = %+v, want a tool TimeoutError", res.ToolResults)
	}
}
