package harness

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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

	// pinCalls/releaseCalls and onPin support
	// TestAgent_StopWhenReleasesCheckpointPinOnAbort's regression coverage
	// for run_prompt.go's checkpoint-pin release guarantee: every
	// mockPromptControl implements harness.CheckpointPinner (matching every
	// real bridge adapter's promptControl), so ordinary StopWhen tests also
	// exercise the pin/release path even though they don't inspect it.
	pinCalls     int
	releaseCalls int
	onPin        func()
}

// PinCheckpoint implements harness.CheckpointPinner, mirroring the real
// bridge adapters' promptControl.PinCheckpoint used by run_prompt.go's
// pendingStopBoundary handling.
func (c *mockPromptControl) PinCheckpoint() (release func()) {
	c.mu.Lock()
	c.pinCalls++
	c.mu.Unlock()
	if c.onPin != nil {
		c.onPin()
	}
	return func() {
		c.mu.Lock()
		c.releaseCalls++
		c.mu.Unlock()
	}
}

func (c *mockPromptControl) checkpointCounts() (pins, releases int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pinCalls, c.releaseCalls
}

var _ CheckpointPinner = (*mockPromptControl)(nil)

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

// mockSteerablePromptControl wraps mockPromptControl and additionally
// implements UserMessageSubmitter, mirroring TS mockHarness's
// `supportsSteering` option: since Go interface satisfaction is static
// (unlike TS's runtime-conditional `submitUserMessage` property), "a
// harness that does/doesn't support steering" is expressed as two distinct
// PromptControl types instead of one control with a nullable method.
type mockSteerablePromptControl struct {
	*mockPromptControl
	userMessages *[]string
}

func (c *mockSteerablePromptControl) SubmitUserMessage(_ context.Context, text string) error {
	c.mu.Lock()
	*c.userMessages = append(*c.userMessages, text)
	c.mu.Unlock()
	return nil
}

type mockHarnessOptions struct {
	script           func(submit func(toolCallID string, output interface{})) []StreamPart
	continueScript   func(submit func(toolCallID string, output interface{})) []StreamPart
	builtinTools     map[string]BuiltinTool
	onSubmitResult   func(ToolResultSubmission)
	supportsApproval bool
	supportsSteering bool
	// promptDone, when set, is called once per DoPromptTurn/DoContinueTurn
	// invocation; the mock keeps that turn "running" (PromptControl.Done()
	// stays open) until the returned channel closes, mirroring TS
	// mockHarness's `promptDone` hook — a window for the test to call
	// ExperimentalSteer while the turn is provably still active.
	promptDone func() <-chan struct{}
	// doSuspendTurn, when set, overrides mockSession's default
	// (always-succeeding) DoSuspendTurn — used to exercise
	// suspendOrFinishNow's fallback-to-hard-finish path.
	doSuspendTurn func(context.Context) (*ContinueTurnState, error)
	// onControl, when set, is called synchronously with each turn's
	// underlying *mockPromptControl as soon as DoPromptTurn/DoContinueTurn
	// creates it, letting a test observe pin/release counts.
	onControl func(*mockPromptControl)
	// onPin, when set, becomes every turn's mockPromptControl.onPin hook.
	onPin func()
}

type mockHarnessResult struct {
	harness         Harness
	session         Session
	toolResults     []recordedToolResult
	toolApprovals   []recordedApproval
	prompts         []Prompt
	turnSettings    []TurnSettings
	responseFormats []*ResponseFormat
	userMessages    []string
}

// newMockHarness builds a mock Harness whose Session emits opts.script on a
// goroutine after DoPromptTurn returns, exactly like the TS mock harness.
func newMockHarness(opts mockHarnessOptions) *mockHarnessResult {
	res := &mockHarnessResult{}
	submit := func(toolCallID string, output interface{}) { _ = toolCallID; _ = output }

	// newControl builds this turn's PromptControl (steerable when
	// opts.supportsSteering) and returns the underlying mockPromptControl
	// too, so the emitting goroutine below can always reach `done`/`mu`
	// regardless of which wrapper type was returned to the caller.
	newControl := func() (PromptControl, *mockPromptControl) {
		base := &mockPromptControl{
			toolResults: &res.toolResults, toolApprovals: &res.toolApprovals,
			onSubmitResult: opts.onSubmitResult, done: make(chan struct{}), onPin: opts.onPin,
		}
		if opts.onControl != nil {
			opts.onControl(base)
		}
		if !opts.supportsSteering {
			return base, base
		}
		return &mockSteerablePromptControl{mockPromptControl: base, userMessages: &res.userMessages}, base
	}

	sess := &mockSession{
		id:            "mock-session-1",
		doSuspendTurn: opts.doSuspendTurn,
		doPromptTurn: func(ctx context.Context, o PromptTurnOptions) (PromptControl, error) {
			res.prompts = append(res.prompts, o.Prompt)
			res.turnSettings = append(res.turnSettings, o.TurnSettings)
			res.responseFormats = append(res.responseFormats, o.ResponseFormat)
			control, base := newControl()
			parts := opts.script(submit)
			var done <-chan struct{}
			if opts.promptDone != nil {
				done = opts.promptDone()
			}
			go func() {
				for _, p := range parts {
					o.Emit(p)
				}
				if done != nil {
					<-done
				}
				close(base.done)
			}()
			return control, nil
		},
		doContinueTurn: func(ctx context.Context, o ContinueTurnOptions) (PromptControl, error) {
			res.responseFormats = append(res.responseFormats, o.ResponseFormat)
			control, base := newControl()
			var parts []StreamPart
			if opts.continueScript != nil {
				parts = opts.continueScript(submit)
			}
			var done <-chan struct{}
			if opts.promptDone != nil {
				done = opts.promptDone()
			}
			go func() {
				for _, p := range parts {
					o.Emit(p)
				}
				if done != nil {
					<-done
				}
				close(base.done)
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
	doSuspendTurn  func(context.Context) (*ContinueTurnState, error)
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
func (s *mockSession) DoSuspendTurn(ctx context.Context) (*ContinueTurnState, error) {
	if s.doSuspendTurn != nil {
		return s.doSuspendTurn(ctx)
	}
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

	mu         sync.Mutex
	startCalls []StartOptions
}

func (h *mockHarnessAdapter) SpecificationVersion() string { return SpecificationVersion }
func (h *mockHarnessAdapter) HarnessID() string            { return h.id }
func (h *mockHarnessAdapter) BuiltinTools() map[string]BuiltinTool {
	if h.builtinTools == nil {
		return map[string]BuiltinTool{}
	}
	return h.builtinTools
}
func (h *mockHarnessAdapter) DoStart(_ context.Context, opts StartOptions) (Session, error) {
	h.mu.Lock()
	h.startCalls = append(h.startCalls, opts)
	h.mu.Unlock()
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

// TestAgent_SettingsInstructionsAcceptsSystemMessage ports TS 4d1bf28
// ("support `instructions` on `HarnessAgent` to be a `SystemModelMessage`
// for parity with `ToolLoopAgent`"): AgentSettings.Instructions accepts a
// *types.Message (a system message) in addition to a plain string, and only
// its Content text reaches the harness adapter's TurnSettings.Instructions.
func TestAgent_SettingsInstructionsAcceptsSystemMessage(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness,
		Instructions: &types.Message{
			Role:    types.RoleSystem,
			Content: []types.ContentPart{types.TextContent{Text: "Be concise."}},
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(mock.turnSettings) != 1 {
		t.Fatalf("turnSettings = %+v, want 1 recorded turn", mock.turnSettings)
	}
	if got := mock.turnSettings[0].Instructions; got != "Be concise." {
		t.Fatalf("TurnSettings.Instructions = %q, want %q (extracted from the *types.Message's Content)", got, "Be concise.")
	}
}

// TestAgent_PrepareCallInstructionsAcceptsSystemMessage ports the same TS
// 4d1bf28 parity for the PrepareCall path: a prepareCall hook may return a
// *types.Message for PrepareCallResult.Instructions too (mirrors TS's
// harness-agent.test.ts "prepares model, skills, instructions, tools ... for
// each fresh turn", where prepareCall returns `instructions: { role:
// 'system', content: ... }`), and it is extracted the same way.
func TestAgent_PrepareCallInstructionsAcceptsSystemMessage(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{
		Harness:      mock.harness,
		Instructions: "default instructions",
		PrepareCall: func(ctx context.Context, opts PrepareCallOptions) (PrepareCallResult, error) {
			return PrepareCallResult{
				HasInstructions: true,
				Instructions: &types.Message{
					Role:    types.RoleSystem,
					Content: []types.ContentPart{types.TextContent{Text: "Serve "}, types.TextContent{Text: "the tenant."}},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(mock.turnSettings) != 1 {
		t.Fatalf("turnSettings = %+v, want 1 recorded turn", mock.turnSettings)
	}
	if got := mock.turnSettings[0].Instructions; got != "Serve the tenant." {
		t.Fatalf("TurnSettings.Instructions = %q, want %q", got, "Serve the tenant.")
	}
}

// TestAgent_SurfacesEveryApprovalFromACountedToolCallStep ports TS 32349cc's
// regression test ("surfaces every approval request from a counted tool-call
// step"): when a step's tool-call events all carry the same
// StepToolCallCount, the host collects every approval request from that step
// before pausing once, instead of pausing after the first one and leaving
// the rest permanently stuck — the exact deadlock 32349cc fixed.
func TestAgent_SurfacesEveryApprovalFromACountedToolCallStep(t *testing.T) {
	two := 2
	var pendingOrder []string
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`, StepToolCallCount: &two},
				&ToolCallPart{ToolCallID: "c2", ToolName: "weather", Input: `{"city":"NYC"}`, StepToolCallCount: &two},
			}
		},
	})
	weather := types.Tool{
		Name: "weather", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"temperature": 72}, nil
		},
	}
	toolApproval := ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval}
	callbacks := Callbacks{}
	a, err := NewAgent(AgentSettings{Harness: mock.harness, UserTools: map[string]types.Tool{"weather": weather}, Callbacks: callbacks, ToolApproval: toolApproval})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	approvals, _, _ := session.snapshotPendingState()
	if len(approvals) != 2 {
		t.Fatalf("pending approvals = %+v, want 2 (both c1 and c2, not just the first)", approvals)
	}
	seen := map[string]bool{}
	for _, approval := range approvals {
		pendingOrder = append(pendingOrder, approval.ToolCallID)
		seen[approval.ToolCallID] = true
	}
	if !seen["c1"] || !seen["c2"] {
		t.Fatalf("pending approval tool call IDs = %v, want both c1 and c2", pendingOrder)
	}

	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("Steps() = %d, want 1", len(steps))
	}
	if len(steps[0].ToolCalls) != 2 {
		t.Fatalf("Steps()[0].ToolCalls = %+v, want both tool calls in the single (paused) step", steps[0].ToolCalls)
	}
}

// TestAgent_PausesOnFirstApprovalWithoutStepToolCallCount verifies the
// backward-compatible default: an adapter that never populates
// StepToolCallCount on its tool-call events keeps the original
// pause-on-first behavior — only the first of two approval-required tool
// calls is surfaced before the turn pauses, matching pre-32349cc behavior
// for adapters that cannot know a step's tool-call cardinality up front.
func TestAgent_PausesOnFirstApprovalWithoutStepToolCallCount(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`},
				&ToolCallPart{ToolCallID: "c2", ToolName: "weather", Input: `{"city":"NYC"}`},
			}
		},
	})
	weather := types.Tool{Name: "weather", Parameters: map[string]any{"type": "object"}}
	toolApproval := ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval}
	a, err := NewAgent(AgentSettings{Harness: mock.harness, UserTools: map[string]types.Tool{"weather": weather}, ToolApproval: toolApproval})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	approvals, _, _ := session.snapshotPendingState()
	if len(approvals) != 1 || approvals[0].ToolCallID != "c1" {
		t.Fatalf("pending approvals = %+v, want only c1 (pause-on-first, unaware of c2)", approvals)
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
	// The mock session's DoSuspendTurn always succeeds, so the StopWhen
	// early-stop suspends the underlying turn (session.go
	// captureStopConditionBoundary) rather than hard-finishing it: the
	// session must stay unfinished/resumable, not return to idle.
	if !session.HasUnfinishedTurn() {
		t.Fatal("session should have an unfinished (suspended) turn after a StopWhen early-stop")
	}
}

// TestAgent_StopWhenSuspendedTurnIsResumable ports the checkpoint-semantics
// half of TS's StopWhen contract that TestAgent_StopWhenStopsBeforeFurtherSteps
// doesn't cover: a turn stopped early by StopWhen isn't just locally
// truncated, it stays resumable through the ordinary
// ContinueGenerate/ContinueStream path, exactly like a turn paused for a
// tool approval or client tool result.
func TestAgent_StopWhenSuspendedTurnIsResumable(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "first"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				// One event of lookahead (TS's "one event of lookahead"
				// refinement / pendingStopBoundary): the driver peeks at
				// this next event before deciding whether to suspend.
				// Reusing a finish-step here (rather than a natural finish)
				// mirrors TS's own analogous test
				// ("generate() stops after a configured step..."), which
				// reuses `finishEvents()[0]` as both the trigger and the
				// lookahead peek.
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
			}
		},
		continueScript: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&TextDeltaPart{ID: "t2", Delta: "second"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	// Fires only on the *first* call this agent ever makes: AgentSettings.
	// StopWhen is fixed for the agent's lifetime, but each Stream/
	// ContinueStream call gets its own fresh turnDriver (and so its own
	// fresh Steps() count starting back at 0) — plain ai.IsStepCount(1)
	// would therefore also match the continued call's own first step and
	// suspend it right back, never reaching its natural finish. A resumed
	// turn genuinely completing (rather than being suspended a second time)
	// is exactly what this test needs to observe.
	var stoppedOnce bool
	stopOnceAtStepOne := ai.StopCondition(func(state ai.StopConditionState) string {
		if stoppedOnce || len(state.Steps) < 1 {
			return ""
		}
		stoppedOnce = true
		return "stop once"
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, StopWhen: []ai.StopCondition{stopOnceAtStepOne}})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	first, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := first.Err(); err != nil {
		t.Fatalf("first.Err() = %v", err)
	}
	if !session.HasUnfinishedTurn() {
		t.Fatal("session should have an unfinished (suspended) turn after a StopWhen early-stop")
	}

	// Directly asserts the underlying resumability contract: the caller can
	// keep going with ContinueGenerate, and it reaches the harness's
	// continueScript exactly as it would after a tool-approval/tool-result
	// pause.
	continued, err := a.ContinueGenerate(context.Background(), agent.AgentGenerateOptions{HarnessSession: session}, nil, nil)
	if err != nil {
		t.Fatalf("ContinueGenerate: %v", err)
	}
	if continued.Text != "second" {
		t.Fatalf("continued.Text = %q, want %q", continued.Text, "second")
	}
	if session.HasUnfinishedTurn() {
		t.Fatal("session should be idle after the resumed turn finishes")
	}
}

// TestAgent_StopWhenReleasesCheckpointPinOnAbort is the regression test for
// run_prompt.go's checkpoint-pin release guarantee (WG13, 31742b9a1b):
// consumeLoop must release a pin taken at a StopWhen-eligible finish-step on
// *every* exit path, not just the two it decides on explicitly (a matching
// StopCondition, or the harness's own natural `finish` arriving right after).
// Before this test's fix, a caller-cancelled ctx racing the "one event of
// lookahead" read (consumeLoop's `case <-d.ctx.Done(): return ...`) skipped
// the release entirely, leaking the bridge channel's pinned replay
// checkpoint forever. Mirrors TS run-prompt.ts's top-level
// `try { ... } finally { releasePendingStopBoundary(); }` around the whole
// read loop, which Go's `defer d.releasePendingStopBoundary()` now matches.
func TestAgent_StopWhenReleasesCheckpointPinOnAbort(t *testing.T) {
	var ctrl *mockPromptControl
	pinned := make(chan struct{})
	var pinnedOnce sync.Once
	unblock := make(chan struct{})

	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "first"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
			}
		},
		// Keeps the turn "running" (no further parts, control.Done() stays
		// open) past the qualifying finish-step, so consumeLoop's next read
		// has nothing else to observe until the test cancels ctx — forcing
		// the ctx.Done() branch of the select, never the "next part" one.
		promptDone: func() <-chan struct{} { return unblock },
		onControl:  func(c *mockPromptControl) { ctrl = c },
		onPin:      func() { pinnedOnce.Do(func() { close(pinned) }) },
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, StopWhen: []ai.StopCondition{ai.IsStepCount(1)}})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result, err := a.Stream(ctx, agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	select {
	case <-pinned:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PinCheckpoint (finish-step never processed)")
	}
	cancel()
	close(unblock) // let the mock goroutine finish so it doesn't leak.

	// Drain the result to force the turn to fully settle before inspecting
	// the mock control's counters, exactly like
	// TestAgent_CallerCancelSettlesWithAbortNotError.
	uiChunks, errs := result.ToUIMessageStream(context.Background())
	for range uiChunks {
	}
	<-errs

	if ctrl == nil {
		t.Fatal("onControl was never called")
	}
	pins, releases := ctrl.checkpointCounts()
	if pins != 1 {
		t.Fatalf("pinCalls = %d, want 1", pins)
	}
	if releases != 1 {
		t.Fatalf("releaseCalls = %d, want 1 (the pin must be released even though the turn ended via ctx cancellation, not a StopWhen decision)", releases)
	}
}

// TestAgent_StopWhenFallsBackToHardFinishWhenSuspendUnsupported ports the
// other half of suspendOrFinishNow's contract: when the adapter's
// DoSuspendTurn fails (e.g. CapabilityUnsupportedError), the local result
// still settles the same way, but the session falls back to WG4's original
// hard-finish behavior — OnTurnFinished fires and the session returns to
// idle — instead of getting stuck reporting an unfinished turn nothing can
// ever resume.
func TestAgent_StopWhenFallsBackToHardFinishWhenSuspendUnsupported(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "first"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				// One event of lookahead — see
				// TestAgent_StopWhenSuspendedTurnIsResumable's identical
				// comment.
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
			}
		},
		doSuspendTurn: func(context.Context) (*ContinueTurnState, error) {
			return nil, NewCapabilityUnsupportedError("mock harness cannot suspend turns", "mock", nil)
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
		t.Fatalf("Text() = %q, want %q", result.Text(), "first")
	}
	if session.HasUnfinishedTurn() {
		t.Fatal("session should be idle: DoSuspendTurn failed, so this must fall back to a hard finish")
	}
}

// TestAgent_CreateSession_CallerOwnedSandboxRunsOnBootstrap ports the
// 31742b9a1b gap fix: sandboxConfig.OnBootstrap previously never ran when
// the caller supplied its own SandboxSession to CreateSession (only the
// harness's own bootstrap recipe did) — it must now run unconditionally,
// the same as the provider-managed paths, mirroring TS `HarnessAgent.
// createSession`'s unconditional post-branch `runSandboxBootstrap` call.
func TestAgent_CreateSession_CallerOwnedSandboxRunsOnBootstrap(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: func(func(string, interface{})) []StreamPart { return nil }})
	calls := 0
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness,
		SandboxConfig: SandboxConfig{
			BootstrapHash: "v1",
			OnBootstrap: func(context.Context, SandboxBootstrapContext) error {
				calls++
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	sb := newMockSandbox()
	if _, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: sb}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if calls != 1 {
		t.Fatalf("OnBootstrap called %d times, want 1", calls)
	}

	// A second session against the same physical sandbox must not re-run
	// OnBootstrap (marker-guarded), the same idempotency
	// CreateHarnessSandboxTemplate relies on.
	if _, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: sb}); err != nil {
		t.Fatalf("CreateSession (second): %v", err)
	}
	if calls != 1 {
		t.Fatalf("OnBootstrap called %d times after a second CreateSession, want 1 (marker should skip it)", calls)
	}
}

// TestAgent_HasOutput mirrors TS `HarnessAgent.hasOutput`'s
// `this.settings.output != null`.
func TestAgent_HasOutput(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: func(func(string, interface{})) []StreamPart { return nil }})

	withoutOutput, err := NewAgent(AgentSettings{Harness: mock.harness})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if withoutOutput.HasOutput() {
		t.Fatalf("HasOutput() = true, want false when Output is unset")
	}

	withOutput, err := NewAgent(AgentSettings{Harness: mock.harness, Output: ai.JSONOutput(ai.JSONOutputOptions{Name: "data"})})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if !withOutput.HasOutput() {
		t.Fatalf("HasOutput() = false, want true when Output is set")
	}
}

// TestAgent_StructuredOutput ports the structured-output wiring TS
// 62a9c2a added to HarnessAgent: (1) the configured Output derives the
// harness-v1 ResponseFormat sent on the wire with every turn (asserted via
// the mock's recorded o.ResponseFormat, mirroring
// `HarnessAgent._resolveResponseFormat`), and (2) the finished turn's Output
// parses the final step's text with that same Output spec, surfaced through
// both StreamTextResult.Output() (Stream) and GenerateTextResult.Output
// (Generate) — mirrors `HarnessAgent._toGenerateResult` awaiting
// `streamResult.output` only when `settings.output != null`.
func TestAgent_StructuredOutput(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: `{"greeting":`},
				&TextDeltaPart{ID: "t1", Delta: `"hi"}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	output := ai.JSONOutput(ai.JSONOutputOptions{Name: "greeting"})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Output: output})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	result, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(mock.responseFormats) != 1 || mock.responseFormats[0] == nil {
		t.Fatalf("responseFormats = %+v, want one non-nil entry", mock.responseFormats)
	}
	if mock.responseFormats[0].Type != ResponseFormatJSON {
		t.Fatalf("ResponseFormat.Type = %q, want %q", mock.responseFormats[0].Type, ResponseFormatJSON)
	}
	if mock.responseFormats[0].Name != "greeting" {
		t.Fatalf("ResponseFormat.Name = %q, want %q", mock.responseFormats[0].Name, "greeting")
	}

	parsed, ok := result.Output.(map[string]interface{})
	if !ok {
		t.Fatalf("GenerateTextResult.Output = %#v (%T), want map[string]interface{}", result.Output, result.Output)
	}
	if parsed["greeting"] != "hi" {
		t.Fatalf("Output[\"greeting\"] = %v, want %q", parsed["greeting"], "hi")
	}
}

// TestAgent_StructuredOutput_StreamsPartial ports TS harness-agent.test.ts
// "streams partial typed output": a.Stream's *ai.StreamTextResult PartialOutput()
// updates as the structured-output text streams in, and Output()/OutputErr()
// resolve to the final parsed value once the turn settles — through the same
// ai.ExternalStreamOptions.Output wiring TestNewStreamTextResultFromParts_Output
// exercises directly, but end to end via AgentSettings.Output/runPrompt.
func TestAgent_StructuredOutput_StreamsPartial(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "structured", Delta: `{"answer":`},
				&TextDeltaPart{ID: "structured", Delta: `"yes"}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	output := ai.JSONOutput(ai.JSONOutputOptions{Name: "answer"})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Output: output})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "answer", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if err := result.OutputErr(); err != nil {
		t.Fatalf("OutputErr() = %v, want nil", err)
	}

	final, ok := result.Output().(map[string]interface{})
	if !ok || final["answer"] != "yes" {
		t.Fatalf("Output() = %#v, want map with answer=yes", result.Output())
	}
	partial, ok := result.PartialOutput().(map[string]interface{})
	if !ok {
		t.Fatalf("PartialOutput() = %#v (%T), want map[string]interface{} (a partial captured while streaming)", result.PartialOutput(), result.PartialOutput())
	}
	if partial["answer"] != "yes" {
		t.Fatalf("PartialOutput()[\"answer\"] = %v, want %q", partial["answer"], "yes")
	}
}

// TestAgent_StructuredOutput_TextResponseFormat mirrors
// `_resolveResponseFormat`'s `responseFormat.type === 'text'` branch: an
// Output spec that resolves to a text format (ai.TextOutput) is still sent
// as an explicit `{type:"text"}` ResponseFormat, distinct from HasOutput
// being false (no Output configured at all sends a nil ResponseFormat).
func TestAgent_StructuredOutput_TextResponseFormat(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "hello"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Output: ai.TextOutput()})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if !a.HasOutput() {
		t.Fatalf("HasOutput() = false, want true")
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(mock.responseFormats) != 1 || mock.responseFormats[0] == nil {
		t.Fatalf("responseFormats = %+v, want one non-nil entry", mock.responseFormats)
	}
	if mock.responseFormats[0].Type != ResponseFormatText {
		t.Fatalf("ResponseFormat.Type = %q, want %q", mock.responseFormats[0].Type, ResponseFormatText)
	}
}

// TestAgent_NoOutputSendsNilResponseFormat ensures an agent with no Output
// configured sends a nil ResponseFormat on the wire (the common,
// pre-62a9c2a path must be unaffected).
func TestAgent_NoOutputSendsNilResponseFormat(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "hello"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	if a.HasOutput() {
		t.Fatalf("HasOutput() = true, want false")
	}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(mock.responseFormats) != 1 || mock.responseFormats[0] != nil {
		t.Fatalf("responseFormats = %+v, want one nil entry", mock.responseFormats)
	}
}

// steerTestScript is a minimal successful single-step turn used by the
// ExperimentalSteer tests below: TS's mock harness emits its script's parts
// immediately (a microtask, independent of when control.Done()/`done`
// resolves), so a turn only actually settles once both have happened. These
// tests gate control.Done() behind their own promptDone channel to hold the
// turn "running" for a steering window, and rely on this script to give the
// eventual close of that channel a well-formed turn to complete instead of
// the "adapter ended the turn without emitting `finish`" protocol error.
func steerTestScript(func(string, interface{})) []StreamPart {
	return []StreamPart{
		&StreamStartPart{},
		&TextDeltaPart{ID: "t1", Delta: "ok"},
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
		&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
	}
}

// TestAgent_ExperimentalSteer_SubmitsToRunningTurn ports TS
// harness-agent.test.ts "experimental_steer() submits a message to the
// running turn".
func TestAgent_ExperimentalSteer_SubmitsToRunningTurn(t *testing.T) {
	finishPrompt := make(chan struct{})
	mock := newMockHarness(mockHarnessOptions{
		script:           steerTestScript,
		supportsSteering: true,
		promptDone:       func() <-chan struct{} { return finishPrompt },
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Start.", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if err := a.ExperimentalSteer(context.Background(), session, "Change course."); err != nil {
		t.Fatalf("ExperimentalSteer: %v", err)
	}

	close(finishPrompt)
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if len(mock.userMessages) != 1 || mock.userMessages[0] != "Change course." {
		t.Fatalf("userMessages = %v, want [%q]", mock.userMessages, "Change course.")
	}
}

// TestAgentSession_ExperimentalSteerTurn ports TS harness-agent.test.ts
// "experimental_steerTurn() exposes the session-level steering API".
func TestAgentSession_ExperimentalSteerTurn(t *testing.T) {
	finishPrompt := make(chan struct{})
	mock := newMockHarness(mockHarnessOptions{
		script:           steerTestScript,
		supportsSteering: true,
		promptDone:       func() <-chan struct{} { return finishPrompt },
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Start.", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if err := session.ExperimentalSteerTurn(context.Background(), "Change course."); err != nil {
		t.Fatalf("ExperimentalSteerTurn: %v", err)
	}

	close(finishPrompt)
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if len(mock.userMessages) != 1 || mock.userMessages[0] != "Change course." {
		t.Fatalf("userMessages = %v, want [%q]", mock.userMessages, "Change course.")
	}
}

// TestAgent_ExperimentalSteer_UnsupportedCapability ports TS
// "experimental_steer() reports an unsupported harness capability": a
// PromptControl that does not implement UserMessageSubmitter surfaces
// CapabilityUnsupportedError.
func TestAgent_ExperimentalSteer_UnsupportedCapability(t *testing.T) {
	finishPrompt := make(chan struct{})
	mock := newMockHarness(mockHarnessOptions{
		script:     steerTestScript,
		promptDone: func() <-chan struct{} { return finishPrompt },
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Start.", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	err = a.ExperimentalSteer(context.Background(), session, "Change course.")
	var capErr *CapabilityUnsupportedError
	if !errors.As(err, &capErr) {
		t.Fatalf("ExperimentalSteer err = %v (%T), want *CapabilityUnsupportedError", err, err)
	}

	close(finishPrompt)
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

// TestAgent_ExperimentalSteer_NoRunningTurn ports TS "experimental_steer()
// rejects when the session has no running turn".
func TestAgent_ExperimentalSteer_NoRunningTurn(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: func(func(string, interface{})) []StreamPart { return nil }})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	err := a.ExperimentalSteer(context.Background(), session, "Change course.")
	if err == nil || !strings.Contains(err.Error(), "has no running turn to steer") {
		t.Fatalf("ExperimentalSteer err = %v, want \"has no running turn to steer\"", err)
	}
}

// TestAgent_ExperimentalSteer_RejectsDuringApprovalPause ports TS
// "experimental_steer() rejects while the turn awaits tool approval": once a
// turn has paused (no longer "running"), steering is rejected even though
// HasUnfinishedTurn() is still true.
func TestAgent_ExperimentalSteer_RejectsDuringApprovalPause(t *testing.T) {
	weather := types.Tool{
		Name: "weather", Description: "gets the weather", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"city": "Paris"}, nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "call-1", ToolName: "weather", Input: `{"city":"Paris"}`},
			}
		},
		supportsSteering: true,
	})
	toolApproval := ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval}
	a, session := newTestAgent(t, mock, map[string]types.Tool{"weather": weather}, Callbacks{}, toolApproval)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Start.", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if !session.HasUnfinishedTurn() {
		t.Fatal("session should have an unfinished (awaiting-approval) turn")
	}

	err = a.ExperimentalSteer(context.Background(), session, "Change course.")
	if err == nil || !strings.Contains(err.Error(), "has no running turn to steer") {
		t.Fatalf("ExperimentalSteer err = %v, want \"has no running turn to steer\"", err)
	}
	if len(mock.userMessages) != 0 {
		t.Fatalf("userMessages = %v, want none", mock.userMessages)
	}
}

// TestAgent_ExperimentalSteer_TargetsCurrentTurnAcrossSequentialTurns ports
// TS "experimental_steer() targets the current turn when a session is
// reused": a session's second turn must not be steerable through a stale
// reference to the first turn's PromptControl (guarded by the turnID
// tracked in session.go's steerHandoff).
func TestAgent_ExperimentalSteer_TargetsCurrentTurnAcrossSequentialTurns(t *testing.T) {
	var mu sync.Mutex
	var finishPrompts []chan struct{}
	mock := newMockHarness(mockHarnessOptions{
		script:           steerTestScript,
		supportsSteering: true,
		promptDone: func() <-chan struct{} {
			ch := make(chan struct{})
			mu.Lock()
			finishPrompts = append(finishPrompts, ch)
			mu.Unlock()
			return ch
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	first, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "First.", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream (first): %v", err)
	}
	if err := a.ExperimentalSteer(context.Background(), session, "Steer first."); err != nil {
		t.Fatalf("ExperimentalSteer (first): %v", err)
	}
	mu.Lock()
	close(finishPrompts[0])
	mu.Unlock()
	if err := first.Err(); err != nil {
		t.Fatalf("first.Err() = %v", err)
	}

	second, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Second.", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream (second): %v", err)
	}
	if err := a.ExperimentalSteer(context.Background(), session, "Steer second."); err != nil {
		t.Fatalf("ExperimentalSteer (second): %v", err)
	}
	mu.Lock()
	close(finishPrompts[1])
	mu.Unlock()
	if err := second.Err(); err != nil {
		t.Fatalf("second.Err() = %v", err)
	}

	if len(mock.userMessages) != 2 || mock.userMessages[0] != "Steer first." || mock.userMessages[1] != "Steer second." {
		t.Fatalf("userMessages = %v, want [%q %q]", mock.userMessages, "Steer first.", "Steer second.")
	}
}

// TestAgent_ExperimentalSteer_RejectsAfterExplicitSuspend ports TS
// harness-agent.test.ts "experimental_steer() rejects after the active turn
// is suspended": AgentSession.SuspendTurn (unlike the internal StopWhen
// early-stop path) always detaches the local session handle, mirroring TS
// `suspendTurn()`'s `finally { endLocalHandle({sessionState: 'detached'}) }`
// — a subsequent steer must be rejected, and the still in-flight turn must
// still settle normally once its own gate is released (this session's
// finishTrackedTurn is a no-op once detached, so it must not fight
// SuspendTurn's own state).
func TestAgent_ExperimentalSteer_RejectsAfterExplicitSuspend(t *testing.T) {
	finishPrompt := make(chan struct{})
	var closeOnce sync.Once
	mock := newMockHarness(mockHarnessOptions{
		script:           steerTestScript,
		supportsSteering: true,
		promptDone:       func() <-chan struct{} { return finishPrompt },
		doSuspendTurn: func(context.Context) (*ContinueTurnState, error) {
			closeOnce.Do(func() { close(finishPrompt) })
			return NewContinueTurnState("mock", map[string]any{})
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Start.", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if _, err := session.SuspendTurn(context.Background()); err != nil {
		t.Fatalf("SuspendTurn: %v", err)
	}

	err = a.ExperimentalSteer(context.Background(), session, "Change course.")
	if err == nil || !strings.Contains(err.Error(), "no running turn to steer") {
		t.Fatalf("ExperimentalSteer err = %v, want \"no running turn to steer\"", err)
	}
	if len(mock.userMessages) != 0 {
		t.Fatalf("userMessages = %v, want none", mock.userMessages)
	}

	// The turn that SuspendTurn detached from still settles on its own —
	// its late OnTurnFinished must not resurrect the session's turnState.
	if err := result.Err(); err != nil {
		t.Fatalf("result.Err() = %v", err)
	}
	if session.turnState != TurnStateSuspended {
		t.Fatalf("session.turnState = %v, want %v (SuspendTurn's own state, undisturbed by the detached turn's finish)", session.turnState, TurnStateSuspended)
	}
}

// TestAgentSession_SuspendTurn_DetachesLocalHandle verifies SuspendTurn's own
// direct contract: a second explicit SuspendTurn call on the now-detached
// session is rejected, mirroring TS `suspendTurn`'s
// `sessionState !== 'active'` guard.
func TestAgentSession_SuspendTurn_DetachesLocalHandle(t *testing.T) {
	finishPrompt := make(chan struct{})
	mock := newMockHarness(mockHarnessOptions{
		script:     steerTestScript,
		promptDone: func() <-chan struct{} { return finishPrompt },
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "Start.", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if _, err := session.SuspendTurn(context.Background()); err != nil {
		t.Fatalf("SuspendTurn: %v", err)
	}
	if _, err := session.SuspendTurn(context.Background()); err == nil || !strings.Contains(err.Error(), "is not active") {
		t.Fatalf("second SuspendTurn err = %v, want \"is not active\"", err)
	}

	close(finishPrompt)
	if err := result.Err(); err != nil {
		t.Fatalf("result.Err() = %v", err)
	}
}
