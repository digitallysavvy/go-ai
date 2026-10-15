package harness

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func finishScript(text string) []StreamPart {
	return []StreamPart{
		&TextDeltaPart{ID: "t1", Delta: text},
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
		&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
	}
}

func clientToolMock() (*mockHarnessResult, map[string]types.Tool) {
	clientTool := types.Tool{Name: "askUser", Description: "asks the user", Parameters: map[string]any{"type": "object"}}
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&StreamStartPart{}, &ToolCallPart{ToolCallID: "call_1", ToolName: "askUser", Input: "{}"}}
		},
		continueScript: func(func(string, interface{})) []StreamPart {
			return append([]StreamPart{&ToolResultPart{ToolCallID: "call_1", ToolName: "askUser", Result: "blue"}}, finishScript("blue.")...)
		},
	})
	return mock, map[string]types.Tool{"askUser": clientTool}
}

// TS suspendTurn() returns the adapter state extended with the session's
// pending tool results and the turn settings (addPendingToolState); the
// continuation is then resumable from a fresh session.
func TestSuspendTurnCarriesPendingToolStateAndTurnSettings(t *testing.T) {
	mock, tools := clientToolMock()
	a, session := newTestAgent(t, mock, tools, Callbacks{}, nil)
	instructions := "Be brief."
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{
		Prompt: "what color?", Instructions: &instructions, HarnessSession: session,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}

	continueFrom, err := session.SuspendTurn(context.Background())
	if err != nil {
		t.Fatalf("SuspendTurn: %v", err)
	}
	if len(continueFrom.PendingToolResults) != 1 || continueFrom.PendingToolResults[0].ToolCallID != "call_1" {
		t.Fatalf("PendingToolResults = %+v", continueFrom.PendingToolResults)
	}
	if ts := continueFrom.TurnSettings; ts == nil || ts.Instructions != "Be brief." || len(ts.Tools) != 1 || ts.Tools[0].Name != "askUser" {
		t.Fatalf("TurnSettings = %+v", continueFrom.TurnSettings)
	}

	resumed, err := a.CreateSession(context.Background(), CreateSessionOptions{SessionID: session.SessionID(), ContinueFrom: continueFrom, SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.ContinueGenerate(context.Background(), agent.AgentGenerateOptions{HarnessSession: resumed}, nil,
		[]types.ToolResultContent{{ToolCallID: "call_1", ToolName: "askUser", Result: "blue"}})
	if err != nil {
		t.Fatalf("ContinueGenerate: %v", err)
	}
	if out.Text != "blue." {
		t.Fatalf("Text = %q", out.Text)
	}
}

// TS detach()/stop() on a session with an unfinished turn suspend it and
// return the resume state with `continueFrom` instead of failing.
func TestDetachAndStopWithUnfinishedTurnReturnContinueFrom(t *testing.T) {
	for _, method := range []string{"detach", "stop"} {
		t.Run(method, func(t *testing.T) {
			mock, tools := clientToolMock()
			a, session := newTestAgent(t, mock, tools, Callbacks{}, nil)
			res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "q", HarnessSession: session}})
			if err != nil {
				t.Fatal(err)
			}
			if err := res.Err(); err != nil {
				t.Fatal(err)
			}
			var state *ResumeSessionState
			if method == "detach" {
				state, err = session.Detach(context.Background())
			} else {
				state, err = session.Stop(context.Background())
			}
			if err != nil {
				t.Fatalf("%s: %v", method, err)
			}
			if state.ContinueFrom == nil || len(state.ContinueFrom.PendingToolResults) != 1 {
				t.Fatalf("ContinueFrom = %+v", state.ContinueFrom)
			}
			if err := state.Validate(); err != nil {
				t.Fatal(err)
			}
			// The handle ended.
			if _, err := session.Detach(context.Background()); err == nil || !strings.Contains(err.Error(), "is not active and cannot be detached.") {
				t.Fatalf("second Detach err = %v", err)
			}
			if _, err := session.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "is not active and cannot be stopped.") {
				t.Fatalf("second Stop err = %v", err)
			}
		})
	}
}

// A turn the host suspends while the model step is still open settles
// cleanly: no error, no partial step, and the turn stays unfinished.
func TestSuspendMidStepSettlesWithoutErrorOrPartialStep(t *testing.T) {
	finish := make(chan struct{})
	var once sync.Once
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&StreamStartPart{}, &TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "partial"}}
		},
		promptDone: func() <-chan struct{} { return finish },
		doSuspendTurn: func(context.Context) (*ContinueTurnState, error) {
			once.Do(func() { close(finish) })
			return NewContinueTurnState("mock", map[string]any{})
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "work", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	continueFrom, err := session.SuspendTurn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err != nil {
		t.Fatalf("result.Err() = %v, want nil for a suspended turn", err)
	}
	if steps := res.Steps(); len(steps) != 0 {
		t.Fatalf("Steps = %d, want none", len(steps))
	}
	if !session.HasUnfinishedTurn() {
		t.Fatal("the suspended turn must stay unfinished")
	}
	if continueFrom.TurnSettings == nil {
		t.Fatal("continuation lost the turn settings")
	}
}

// A host tool that completes after the host suspended the turn is kept as a
// completed pending result and submitted, without running again, when the
// turn is continued (TS "retained" tool results).
func TestSuspendKeepsHostToolResultCompletedAfterSuspension(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finish := make(chan struct{})
	suspendCalled := make(chan struct{})
	var executed int
	var mu sync.Mutex
	tool := types.Tool{
		Name: "research", Description: "r", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			executed++
			mu.Unlock()
			close(started)
			<-release
			return "retained", nil
		},
	}
	var once sync.Once
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&StreamStartPart{}, &ToolCallPart{ToolCallID: "call_1", ToolName: "research", Input: "{}"}}
		},
		continueScript: func(func(string, interface{})) []StreamPart { return finishScript("done") },
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

	type suspended struct {
		state *ContinueTurnState
		err   error
	}
	ch := make(chan suspended, 1)
	go func() {
		st, err := session.SuspendTurn(context.Background())
		ch <- suspended{st, err}
	}()
	<-suspendCalled
	close(release)

	var got suspended
	select {
	case got = <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("SuspendTurn did not return")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	if err := res.Err(); err != nil {
		t.Fatalf("result.Err() = %v", err)
	}
	if len(got.state.PendingToolResults) != 1 {
		t.Fatalf("PendingToolResults = %+v", got.state.PendingToolResults)
	}
	pr := got.state.PendingToolResults[0]
	if pr.CompletedResult == nil || pr.CompletedResult.Output != "retained" || pr.Input != "{}" || pr.ToolName != "research" {
		t.Fatalf("pending result = %+v", pr)
	}
	if n := len(mock.toolResults); n != 0 {
		t.Fatalf("the closed runtime received %d tool results, want 0", n)
	}

	resumed, err := a.CreateSession(context.Background(), CreateSessionOptions{SessionID: session.SessionID(), ContinueFrom: got.state, SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ContinueGenerate(context.Background(), agent.AgentGenerateOptions{HarnessSession: resumed}, nil, nil); err != nil {
		t.Fatalf("ContinueGenerate: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if executed != 1 {
		t.Fatalf("tool executed %d times, want 1", executed)
	}
	if len(mock.toolResults) != 1 || mock.toolResults[0].Output != "retained" {
		t.Fatalf("submitted results = %+v", mock.toolResults)
	}
}

// Generate/Stream route a trailing tool message with approval responses or
// tool results to the unfinished turn, like TS generate()/stream().
func TestGenerateRoutesTrailingToolMessageToContinuation(t *testing.T) {
	mock, tools := clientToolMock()
	a, session := newTestAgent(t, mock, tools, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "q", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	out, err := a.Generate(context.Background(), agent.AgentGenerateOptions{
		HarnessSession: session,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "call_1", ToolName: "askUser", Result: "blue"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Text != "blue." {
		t.Fatalf("Text = %q", out.Text)
	}
}

func TestPromptTurnInputUsesLastUserMessage(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: func(func(string, interface{})) []StreamPart { return finishScript("ok") }})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	user := func(s string) types.Message {
		return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: s}}}
	}
	assistant := types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "earlier"}}}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{HarnessSession: session, Messages: []types.Message{user("first"), assistant}}); err != nil {
		t.Fatal(err)
	}
	if len(mock.prompts) != 1 || mock.prompts[0].Message == nil || mock.prompts[0].Message.Content[0].(types.TextContent).Text != "first" {
		t.Fatalf("prompts = %+v, want the last user message", mock.prompts)
	}
	_, err := a.Generate(context.Background(), agent.AgentGenerateOptions{HarnessSession: session, Messages: []types.Message{assistant}})
	if err == nil || err.Error() != "HarnessAgent: messages must contain at least one `role: \"user\"` entry." {
		t.Fatalf("err = %v", err)
	}
	_, err = a.Generate(context.Background(), agent.AgentGenerateOptions{HarnessSession: session})
	if err == nil || err.Error() != "HarnessAgent: either `prompt` or `messages` is required." {
		t.Fatalf("err = %v", err)
	}
}

// A resumed turn needs every persisted tool: TS throws when one is gone.
func TestContinueFailsWhenPersistedToolIsNoLongerAvailable(t *testing.T) {
	mock, tools := clientToolMock()
	a, session := newTestAgent(t, mock, tools, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "q", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	continueFrom, err := session.SuspendTurn(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	b, err := NewAgent(AgentSettings{Harness: mock.harness})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := b.CreateSession(context.Background(), CreateSessionOptions{SessionID: "s", ContinueFrom: continueFrom, SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.ContinueGenerate(context.Background(), agent.AgentGenerateOptions{HarnessSession: resumed}, nil, nil)
	want := "HarnessAgent cannot continue turn because tool 'askUser' is no longer available."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// compact() works on a running turn (TS only requires a reusable session).
func TestCompactDuringRunningTurn(t *testing.T) {
	finish := make(chan struct{})
	mock := newMockHarness(mockHarnessOptions{
		script:     func(func(string, interface{})) []StreamPart { return append([]StreamPart{&StreamStartPart{}}, finishScript("x")...) },
		promptDone: func() <-chan struct{} { return finish },
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Compact(context.Background(), ""); err != nil {
		t.Fatalf("Compact during a running turn: %v", err)
	}
	close(finish)
	_ = res.Err()
	if _, err := session.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Compact(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "has ended and cannot be reused") {
		t.Fatalf("Compact after stop err = %v", err)
	}
}
