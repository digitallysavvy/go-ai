package harness

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A host tool that fails after the host suspended the turn is published as a
// tool-error carrying the submitted output as is (TS enqueueHostToolOutcome).
func TestR47SuspendedHostToolErrorCarriesSubmittedOutput(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finish := make(chan struct{})
	suspendCalled := make(chan struct{})
	tool := types.Tool{
		Name: "research", Description: "r", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			close(started)
			<-release
			return nil, errors.New("boom")
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
	// TS enqueueHostToolOutcome: `error: submission.output`, the submitted
	// `{ error: String(err) }` object, whose getErrorMessage text is JSON.
	if len(results) != 1 || results[0].Error == nil || results[0].Error.Error() != `{"error":"Error: boom"}` {
		t.Fatalf("tool results = %+v, want one tool-error with the submitted output", results)
	}
	if m, ok := results[0].Result.(map[string]interface{}); !ok || m["error"] != "Error: boom" {
		t.Fatalf("tool-error value = %#v", results[0].Result)
	}
}

// The suspended host tool-error keeps the submitted output unstripped: an
// error naming the session work dir reaches the stream with the absolute
// path in both its getErrorMessage text and its value (TS
// enqueueHostToolOutcome passes `outcome.error` without stripWorkDir; only
// successful outputs are stripped).
func TestR47SuspendedHostToolErrorKeepsWorkDirPath(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finish := make(chan struct{})
	suspendCalled := make(chan struct{})
	var workDir string
	tool := types.Tool{
		Name: "research", Description: "r", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			close(started)
			<-release
			return nil, errors.New("cannot open " + workDir + "/notes.md")
		},
	}
	var once sync.Once
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
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
	workDir = session.sessionWorkDir
	if workDir == "" {
		t.Fatal("test needs a session work dir")
	}
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
	want := "Error: cannot open " + workDir + "/notes.md"
	if len(results) != 1 || results[0].Error == nil || results[0].Error.Error() != `{"error":"`+want+`"}` {
		t.Fatalf("tool results = %+v, want one tool-error with the unstripped submitted output", results)
	}
	if m, ok := results[0].Result.(map[string]interface{}); !ok || m["error"] != want {
		t.Fatalf("tool-error value = %#v, want the unstripped %q", results[0].Result, want)
	}
}
