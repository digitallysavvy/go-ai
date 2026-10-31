package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A PrepareCall timeout (TS `timeout ?? preparedCall.timeout`) bounds the
// step's tool executions in the legacy loop, as it bounds the step.
func TestGenerateAgentToolTimeoutFromPrepareCall(t *testing.T) {
	toolTimeout := 20 * time.Millisecond
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
			c.Timeout = &ai.TimeoutConfig{ToolMs: &toolTimeout}
			return c
		},
	})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	// A failed call is a tool-error content part, not a tool result (TS).
	if toolErrors := r71integAgentToolErrors(res.Steps); len(res.ToolResults) != 0 || len(toolErrors) != 1 || !strings.HasPrefix(toolErrors[0].Error.(string), (&ai.TimeoutError{Reason: ai.TimeoutReasonTool}).Error()) {
		t.Fatalf("tool results = %+v, tool errors = %+v, want a tool TimeoutError", res.ToolResults, toolErrors)
	}
}
