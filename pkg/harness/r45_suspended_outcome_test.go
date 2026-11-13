package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A host tool that completes after the host suspended the turn is published
// by TS enqueueHostToolOutcome, whose tool-result carries no providerExecuted,
// even when the originating call set an explicit false.
func TestR45SuspendedHostToolOutcomeOmitsProviderExecuted(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finish := make(chan struct{})
	suspendCalled := make(chan struct{})
	tool := types.Tool{
		Name: "research", Description: "r", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			close(started)
			<-release
			return "done", nil
		},
	}
	var once sync.Once
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			// No ProviderExecutedOmitted: the call sets providerExecuted: false.
			return []StreamPart{&StreamStartPart{}, &ToolCallPart{ToolCallID: "call_1", ToolName: "research", Input: "{}"}}
		},
		promptDone: func() <-chan struct{} {
			select {
			case <-finish:
				return nil
			default:
				return finish
			}
		},
		doSuspendTurn: func(context.Context) (*ContinueTurnState, error) {
			once.Do(func() { close(suspendCalled); close(finish) })
			return NewContinueTurnState("mock", map[string]any{})
		},
	})
	a, session := newTestAgent(t, mock, map[string]types.Tool{"research": tool}, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	done := make(chan error, 1)
	go func() {
		_, err := session.SuspendTurn(context.Background())
		done <- err
	}()
	<-suspendCalled
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var results []types.ToolResult
	stream := res.Stream()
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil {
			results = append(results, *c.ToolResult)
		}
	}
	if len(results) != 1 || results[0].Error != nil || results[0].Result != "done" {
		t.Fatalf("tool results = %+v, want one successful result", results)
	}
	if results[0].ProviderExecuted || results[0].ProviderExecutedSet {
		t.Fatalf("tool result = %+v, want providerExecuted left undefined", results[0])
	}
}
