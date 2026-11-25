package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS runPrompt awaits waitForOutstandingHostToolExecutions after the stream
// ends, finish or not: a host tool whose result could not be submitted fails
// the turn even when the adapter closed its stream without a finish.
func TestR45StreamEndWithoutFinishJoinsHostToolExecutions(t *testing.T) {
	tool := types.Tool{
		Name: "slow", Description: "s", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			time.Sleep(50 * time.Millisecond)
			return "ok", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&StreamStartPart{}, &ToolCallPart{ToolCallID: "call_1", ToolName: "slow", Input: "{}"}}
		},
		submitErr: errors.New("submit failed"),
	})
	a, session := newTestAgent(t, mock, map[string]types.Tool{"slow": tool}, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err == nil || !strings.Contains(err.Error(), "submit failed") {
		t.Fatalf("Err() = %v, want the failed submission", err)
	}
}
