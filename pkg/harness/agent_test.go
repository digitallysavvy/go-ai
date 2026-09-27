package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Ports the shape of TS harness-agent.test.ts's `mockHarness` helper: a
// Harness/Session pair whose DoPromptTurn/DoContinueTurn emits a canned,
// test-author-written script of StreamParts on a goroutine (mirroring the TS
// mock's `queueMicrotask`), and records every tool result / approval the
// host submits back for assertions.

type recordedToolResult struct {
	ToolCallID string
	Output     interface{}
	IsError    bool
}

type recordedApproval struct {
	ApprovalID string
	Approved   bool
	Reason     string
}

type mockPromptControl struct {
	mu             sync.Mutex
	toolResults    *[]recordedToolResult
	toolApprovals  *[]recordedApproval
	onSubmitResult func(ToolResultSubmission)
	done           chan struct{}
	err            error
}

func (c *mockPromptControl) SubmitToolResult(_ context.Context, r ToolResultSubmission) error {
	if c.onSubmitResult != nil {
		c.onSubmitResult(r)
	}
	c.mu.Lock()
	*c.toolResults = append(*c.toolResults, recordedToolResult{ToolCallID: r.ToolCallID, Output: r.Output, IsError: r.IsError})
	c.mu.Unlock()
	return nil
}

func (c *mockPromptControl) SubmitToolApproval(_ context.Context, a ToolApprovalSubmission) error {
	c.mu.Lock()
	*c.toolApprovals = append(*c.toolApprovals, recordedApproval{ApprovalID: a.ApprovalID, Approved: a.Approved, Reason: a.Reason})
	c.mu.Unlock()
	return nil
}

func (c *mockPromptControl) Done() <-chan struct{} { return c.done }
func (c *mockPromptControl) Err() error            { return c.err }

type mockHarnessOptions struct {
	script           func(submit func(toolCallID string, output interface{})) []StreamPart
	continueScript   func(submit func(toolCallID string, output interface{})) []StreamPart
	builtinTools     map[string]BuiltinTool
	onSubmitResult   func(ToolResultSubmission)
	supportsApproval bool
}

type mockHarnessResult struct {
	harness       Harness
	session       Session
	toolResults   []recordedToolResult
	toolApprovals []recordedApproval
	prompts       []Prompt
}

// newMockHarness builds a mock Harness whose Session emits opts.script on a
// goroutine after DoPromptTurn returns, exactly like the TS mock harness.
func newMockHarness(opts mockHarnessOptions) *mockHarnessResult {
	res := &mockHarnessResult{}
	submit := func(toolCallID string, output interface{}) { _ = toolCallID; _ = output }

	sess := &mockSession{
		id: "mock-session-1",
		doPromptTurn: func(ctx context.Context, o PromptTurnOptions) (PromptControl, error) {
			res.prompts = append(res.prompts, o.Prompt)
			control := &mockPromptControl{
				toolResults: &res.toolResults, toolApprovals: &res.toolApprovals,
				onSubmitResult: opts.onSubmitResult, done: make(chan struct{}),
			}
			parts := opts.script(submit)
			go func() {
				for _, p := range parts {
					o.Emit(p)
				}
				close(control.done)
			}()
			return control, nil
		},
		doContinueTurn: func(ctx context.Context, o ContinueTurnOptions) (PromptControl, error) {
			control := &mockPromptControl{
				toolResults: &res.toolResults, toolApprovals: &res.toolApprovals,
				onSubmitResult: opts.onSubmitResult, done: make(chan struct{}),
			}
			var parts []StreamPart
			if opts.continueScript != nil {
				parts = opts.continueScript(submit)
			}
			go func() {
				for _, p := range parts {
					o.Emit(p)
				}
				close(control.done)
			}()
			return control, nil
		},
	}
	res.session = sess

	h := &mockHarnessAdapter{id: "mock", builtinTools: opts.builtinTools, session: sess, supportsApproval: opts.supportsApproval}
	res.harness = h
	return res
}

// mockSession implements Session by delegating to test-supplied funcs.
type mockSession struct {
	id             string
	doPromptTurn   func(context.Context, PromptTurnOptions) (PromptControl, error)
	doContinueTurn func(context.Context, ContinueTurnOptions) (PromptControl, error)
}

func (s *mockSession) SessionID() string { return s.id }
func (s *mockSession) IsResume() bool    { return false }
func (s *mockSession) DoPromptTurn(ctx context.Context, o PromptTurnOptions) (PromptControl, error) {
	return s.doPromptTurn(ctx, o)
}
func (s *mockSession) DoContinueTurn(ctx context.Context, o ContinueTurnOptions) (PromptControl, error) {
	if s.doContinueTurn == nil {
		return nil, nil
	}
	return s.doContinueTurn(ctx, o)
}
func (s *mockSession) DoCompact(context.Context, string) error { return nil }
func (s *mockSession) DoSuspendTurn(context.Context) (*ContinueTurnState, error) {
	return NewContinueTurnState("mock", map[string]any{})
}
func (s *mockSession) DoDetach(context.Context) (*ResumeSessionState, error) {
	return NewResumeSessionState("mock", map[string]any{})
}
func (s *mockSession) DoStop(context.Context) (*ResumeSessionState, error) {
	return NewResumeSessionState("mock", map[string]any{})
}
func (s *mockSession) DoDestroy(context.Context) error { return nil }

type mockHarnessAdapter struct {
	id               string
	builtinTools     map[string]BuiltinTool
	session          Session
	supportsApproval bool
}

func (h *mockHarnessAdapter) SpecificationVersion() string { return SpecificationVersion }
func (h *mockHarnessAdapter) HarnessID() string            { return h.id }
func (h *mockHarnessAdapter) BuiltinTools() map[string]BuiltinTool {
	if h.builtinTools == nil {
		return map[string]BuiltinTool{}
	}
	return h.builtinTools
}
func (h *mockHarnessAdapter) DoStart(context.Context, StartOptions) (Session, error) {
	return h.session, nil
}
func (h *mockHarnessAdapter) SupportsBuiltinToolApprovals() bool { return h.supportsApproval }

func testSandbox() providerutils.SandboxSession { return newMockSandbox() }

func newTestAgent(t *testing.T, mock *mockHarnessResult, userTools map[string]types.Tool, callbacks Callbacks, toolApproval ToolApprovalConfiguration) (*Agent, *AgentSession) {
	t.Helper()
	a, err := NewAgent(AgentSettings{Harness: mock.harness, UserTools: userTools, Callbacks: callbacks, ToolApproval: toolApproval})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return a, session
}

func stringUsage(input, output int) Usage {
	i, o := input, output
	return Usage{InputTokens: InputTokenUsage{Total: &i}, OutputTokens: OutputTokenUsage{Total: &o}}
}

// TestAgent_SimpleTextTurn ports the basic single-step text-only scenario
// present throughout harness-agent.test.ts.
func TestAgent_SimpleTextTurn(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t1"},
				&TextDeltaPart{ID: "t1", Delta: "Hello, "},
				&TextDeltaPart{ID: "t1", Delta: "world."},
				&TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(3, 5)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(3, 5)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Text != "Hello, world." {
		t.Fatalf("Text = %q, want %q", result.Text, "Hello, world.")
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %q", result.FinishReason)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("Steps = %d, want 1", len(result.Steps))
	}
	if got := *result.TotalUsage.TotalTokens; got != 8 {
		t.Fatalf("TotalUsage.TotalTokens = %d, want 8", got)
	}
	if !session.HasUnfinishedTurn() == false {
		// turn should be idle (finished) afterward
	}
	if session.HasUnfinishedTurn() {
		t.Fatalf("session should be idle after a finished turn")
	}
}

// TestAgent_TotalUsageOverridesLocalSum ports the harness.md WG4 / TS
// 57e0a59 requirement: the bridge's own totalUsage on the terminal `finish`
// wins over the sum of the per-step usages.
func TestAgent_TotalUsageOverridesLocalSum(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "a"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				&TextDeltaPart{ID: "t2", Delta: "b"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				// Bridge totalUsage (100) intentionally does not equal the
				// natural sum of the two steps above (4): it must win.
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(50, 50)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("Steps = %d, want 2 (no phantom step for the terminal boundary)", len(result.Steps))
	}
	if got := *result.TotalUsage.TotalTokens; got != 100 {
		t.Fatalf("TotalUsage.TotalTokens = %d, want 100 (overridden, not summed to 4)", got)
	}
}

// TestAgent_UnclosedStepIsProtocolError ports the mandated behavior: a
// terminal `finish` with unclosed step content (no preceding `finish-step`)
// fails the turn instead of being silently accepted.
func TestAgent_UnclosedStepIsProtocolError(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "unflushed"},
				// No FinishStepPart before this: protocol violation.
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err == nil || err.Error() != unclosedStepErrorMessage {
		t.Fatalf("Err() = %v, want %q", err, unclosedStepErrorMessage)
	}
	// Partial content must still be preserved (soft failure), not discarded.
	if result.Text() != "unflushed" {
		t.Errorf("Text() = %q, want %q (partial content preserved)", result.Text(), "unflushed")
	}
}

// TestAgent_HostToolExecution ports a host (client-executed) tool call:
// the tool runs, the adapter echoes the result back as its own tool-result
// event (matching the real harness-v1 contract), and the final text reflects
// it.
func TestAgent_HostToolExecution(t *testing.T) {
	var executed bool
	weatherTool := types.Tool{
		Name: "getWeather", Description: "gets the weather", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return "sunny", nil
		},
	}

	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "call_1", ToolName: "getWeather", Input: "{}"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				// The adapter echoes the host's submitted result back.
				&ToolResultPart{ToolCallID: "call_1", ToolName: "getWeather", Result: "sunny"},
				&TextDeltaPart{ID: "t1", Delta: "It is sunny."},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(4, 4)},
			}
		},
	})
	a, session := newTestAgent(t, mock, map[string]types.Tool{"getWeather": weatherTool}, Callbacks{}, nil)

	result, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "weather?", HarnessSession: session})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !executed {
		t.Fatal("host tool was not executed")
	}
	if result.Text != "It is sunny." {
		t.Fatalf("Text = %q", result.Text)
	}
	if len(mock.toolResults) != 1 || mock.toolResults[0].ToolCallID != "call_1" || mock.toolResults[0].Output != "sunny" {
		t.Fatalf("toolResults = %+v", mock.toolResults)
	}
}

// TestAgent_CustomToolApprovalPauseAndContinue ports the pause/continue
// approval flow for a host tool configured to require approval.
func TestAgent_CustomToolApprovalPauseAndContinue(t *testing.T) {
	var executed bool
	sensitiveTool := types.Tool{
		Name: "deleteFile", Description: "deletes a file", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return "deleted", nil
		},
	}

	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "call_1", ToolName: "deleteFile", Input: "{}"},
			}
		},
		continueScript: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&ToolResultPart{ToolCallID: "call_1", ToolName: "deleteFile", Result: "deleted"},
				&TextDeltaPart{ID: "t1", Delta: "Done."},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	toolApproval := ToolApprovalConfiguration{"deleteFile": ai.ToolApprovalStatusUserApproval}
	a, session := newTestAgent(t, mock, map[string]types.Tool{"deleteFile": sensitiveTool}, Callbacks{}, toolApproval)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "delete it", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if executed {
		t.Fatal("tool must not execute before approval")
	}
	if !session.HasUnfinishedTurn() {
		t.Fatal("session should have an unfinished (awaiting-approval) turn")
	}

	approvals, _, _ := session.snapshotPendingState()
	if len(approvals) != 1 {
		t.Fatalf("pending approvals = %+v, want 1", approvals)
	}
	approvalID := approvals[0].ApprovalID

	continued, err := a.ContinueGenerate(context.Background(), agent.AgentGenerateOptions{HarnessSession: session},
		[]types.ToolApprovalResponseContent{{ApprovalID: approvalID, ToolCallID: "call_1", Approved: true}}, nil)
	if err != nil {
		t.Fatalf("ContinueGenerate: %v", err)
	}
	if !executed {
		t.Fatal("tool should have executed after approval")
	}
	if continued.Text != "Done." {
		t.Fatalf("Text = %q", continued.Text)
	}
	if session.HasUnfinishedTurn() {
		t.Fatal("session should be idle after the turn finishes")
	}
}

// TestAgent_ClientSideToolPauseAndContinue ports a non-executable
// (client-side) tool: the turn pauses awaiting a caller-supplied result via
// ContinueStream's ToolResultContinuations.
func TestAgent_ClientSideToolPauseAndContinue(t *testing.T) {
	clientTool := types.Tool{Name: "askUser", Description: "asks the user something", Parameters: map[string]any{"type": "object"}}

	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "call_1", ToolName: "askUser", Input: "{}"},
			}
		},
		continueScript: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&ToolResultPart{ToolCallID: "call_1", ToolName: "askUser", Result: "blue"},
				&TextDeltaPart{ID: "t1", Delta: "Got it: blue."},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	a, session := newTestAgent(t, mock, map[string]types.Tool{"askUser": clientTool}, Callbacks{}, nil)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "what color?", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	_, results, _ := session.snapshotPendingState()
	if len(results) != 1 || results[0].ToolCallID != "call_1" {
		t.Fatalf("pending results = %+v", results)
	}

	continued, err := a.ContinueStream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{HarnessSession: session},
	}, nil, []types.ToolResultContent{{ToolCallID: "call_1", ToolName: "askUser", Result: "blue"}})
	if err != nil {
		t.Fatalf("ContinueStream: %v", err)
	}
	if err := continued.Err(); err != nil {
		t.Fatalf("continued Err() = %v", err)
	}
	if continued.Text() != "Got it: blue." {
		t.Fatalf("Text() = %q", continued.Text())
	}
}

// TestAgent_CallerCancelSettlesWithAbortNotError ports TS 86a84c9's contract
// at the HarnessAgent level: when the caller's own ctx is already cancelled
// by the time the turn settles — even though the mock harness reports a
// wire-level error, exactly like a real adapter surfacing an AbortError once
// its own subprocess is killed — the turn ends with an "abort" chunk (never
// an "error" one), Err() still returns a non-nil error so an awaiting caller
// does not hang, and the session still returns to idle (OnTurnFailed fires
// either way, matching TS "Both outcomes notify onTurnFailed").
func TestAgent_CallerCancelSettlesWithAbortNotError(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "partial "},
				&ErrorPart{Error: "AbortError: This operation was aborted"},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := a.Stream(ctx, agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Deliberately a fresh, non-cancelled ctx: reading back the already-
	// buffered result (e.g. to finish writing an HTTP response) happens on
	// its own live ctx, independent of the generation ctx that aborted.
	uiChunks, errs := result.ToUIMessageStream(context.Background())
	var chunkTypes []string
	for c := range uiChunks {
		if ty, ok := c["type"].(string); ok {
			chunkTypes = append(chunkTypes, ty)
		}
	}
	if e, ok := <-errs; ok && e != nil {
		t.Fatalf("ToUIMessageStream error = %v, want none (an abort must not surface as an error)", e)
	}

	sawAbort, sawError := false, false
	for _, ty := range chunkTypes {
		if ty == "abort" {
			sawAbort = true
		}
		if ty == "error" {
			sawError = true
		}
	}
	if !sawAbort {
		t.Fatalf("chunk types = %v, want an \"abort\" chunk", chunkTypes)
	}
	if sawError {
		t.Fatalf("chunk types = %v, want no \"error\" chunk", chunkTypes)
	}

	if err := result.Err(); err == nil {
		t.Fatal("Err() = nil, want a non-nil error (accessors still reject with the underlying error)")
	}
	if session.HasUnfinishedTurn() {
		t.Fatal("session should be idle after an aborted turn (OnTurnFailed must still fire)")
	}
}

// TestAgent_WireErrorWithoutCancelStaysAnError verifies the counterpart to
// the above: absent caller cancellation, a wire-level `error` event keeps
// its ordinary ChunkTypeError classification (an "error" chunk, no "abort"
// chunk) — TS's "keeps a real error part when the abort signal has not
// fired".
func TestAgent_WireErrorWithoutCancelStaysAnError(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ErrorPart{Error: "boom"},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err == nil {
		t.Fatal("Err() = nil, want a non-nil error")
	}

	uiChunks, _ := result.ToUIMessageStream(context.Background())
	sawAbort, sawError := false, false
	for c := range uiChunks {
		switch c["type"] {
		case "abort":
			sawAbort = true
		case "error":
			sawError = true
		}
	}
	if sawAbort {
		t.Fatal("chunk types include \"abort\", want none (no caller cancellation occurred)")
	}
	if !sawError {
		t.Fatal("chunk types do not include \"error\", want one")
	}
	if session.HasUnfinishedTurn() {
		t.Fatal("session should be idle after a failed turn")
	}
}

// TestAgent_StopWhenStopsBeforeFurtherSteps ports StopWhen (isStepCount(1)):
// the local result finishes after the first step without waiting for the
// bridge's own terminal finish.
func TestAgent_StopWhenStopsBeforeFurtherSteps(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "first"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				// A real adapter would keep streaming a second step here;
				// the driver must stop consuming after StopWhen matches and
				// never observe it.
				&TextDeltaPart{ID: "t2", Delta: "second (must not appear)"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, StopWhen: []ai.StopCondition{ai.IsStepCount(1)}})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if result.Text() != "first" {
		t.Fatalf("Text() = %q, want %q (stopped before the second step)", result.Text(), "first")
	}
	if len(result.Steps()) != 1 {
		t.Fatalf("Steps() = %d, want 1", len(result.Steps()))
	}
}
