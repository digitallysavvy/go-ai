package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var errR95genAgentAbort = errors.New("r95gen agent abort reason")

// The legacy loop's tool calls follow TS mapWithConcurrency: an abort
// rejects with signal.reason (context.Cause), also when every call started
// under a limit below the call count (the workers check the signal after
// their last call); without a limit (Promise.all) it is not checked.
func TestR95genAgentToolLimitAbortCause(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   *int
		cancel  string
		wantErr bool
	}{
		{"queued call not started", intPtrR95gen(1), "a", true},
		{"every call started", intPtrR95gen(2), "c", true},
		{"unlimited", nil, "c", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewToolLoopAgent(AgentConfig{Model: &testutil.MockLanguageModel{}, ToolCallConcurrency: tc.limit})
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			calls := []types.ToolCall{{ID: "a"}, {ID: "b"}, {ID: "c"}}
			_, err := a.runToolCalls(ctx, calls, func(call types.ToolCall, _ agentToolStartGate) ([]types.ToolResult, error) {
				if call.ID == tc.cancel {
					cancel(errR95genAgentAbort)
				}
				return []types.ToolResult{{ToolCallID: call.ID}}, nil
			})
			if tc.wantErr && err != errR95genAgentAbort {
				t.Fatalf("err = %v, want the abort reason", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want none", err)
			}
		})
	}
}

// A step timeout that expires while a tool swallows the abort ends the run
// with the step TimeoutError, the signal's reason (TS throwIfAborted).
func TestR95genAgentStepTimeoutDuringToolsIsTimeoutError(t *testing.T) {
	step := 30 * time.Millisecond
	a := NewToolLoopAgent(AgentConfig{
		Model: r52ToolCallModel(types.ToolCall{ID: "c1", ToolName: "slow", Arguments: map[string]interface{}{}}),
		Tools: []types.Tool{{
			Name:       "slow",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				select {
				case <-ctx.Done():
					return "swallowed", nil
				case <-time.After(3 * time.Second):
					return "late", nil
				}
			},
		}},
		Timeout: &ai.TimeoutConfig{PerStep: &step},
	})
	_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	var te *ai.TimeoutError
	if !errors.As(err, &te) || te.Reason != ai.TimeoutReasonStep || te.Error() != "Step timeout of 30ms exceeded" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %T %v, want the step TimeoutError", err, err)
	}
}

func intPtrR95gen(v int) *int { return &v }
