package workflow

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ─── fake sandbox (mirrors pkg/harness/agent_test.go's mockSandbox/
// mockNetworkSandbox, scoped locally since those are unexported test types
// there) ────────────────────────────────────────────────────────────────

type fakeSandbox struct{}

func (fakeSandbox) Description() string { return "fake" }
func (fakeSandbox) Run(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	if opts.Command == "pwd" {
		return providerutils.SandboxRunResult{Stdout: "/work\n"}, nil
	}
	return providerutils.SandboxRunResult{}, nil
}
func (fakeSandbox) Spawn(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return nil, errors.New("not supported")
}
func (fakeSandbox) ReadFile(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not supported")
}
func (fakeSandbox) ReadBinaryFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("not supported")
}
func (fakeSandbox) ReadTextFile(context.Context, providerutils.SandboxReadTextFileOptions) (*string, error) {
	return nil, nil
}
func (fakeSandbox) WriteFile(context.Context, string, io.Reader) error    { return nil }
func (fakeSandbox) WriteBinaryFile(context.Context, string, []byte) error { return nil }
func (fakeSandbox) WriteTextFile(context.Context, providerutils.SandboxWriteTextFileOptions) error {
	return nil
}

// fakeNetworkSandbox is the harness.NetworkSandboxSession a
// harness.SandboxProvider hands back — required because
// RunHarnessAgent/RunHarnessAgentTimeSlice call HarnessWorkflowAgent.
// CreateSession without a per-call SandboxSession (mirroring TS
// HarnessWorkflowAgent.createSession, which takes none either): the sandbox
// always comes from the agent-level SandboxProvider.
type fakeNetworkSandbox struct{ fakeSandbox }

func (fakeNetworkSandbox) ID() string                      { return "fake-sandbox" }
func (fakeNetworkSandbox) DefaultWorkingDirectory() string { return "/work" }
func (fakeNetworkSandbox) Ports() []int                    { return nil }
func (fakeNetworkSandbox) GetPortEndpoint(context.Context, harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	return harness.PortEndpoint{}, errors.New("no ports")
}
func (fakeNetworkSandbox) GetPortURL(context.Context, harness.PortEndpointOptions) (string, error) {
	return "", errors.New("no ports")
}
func (fakeNetworkSandbox) Stop(context.Context) error    { return nil }
func (fakeNetworkSandbox) Destroy(context.Context) error { return nil }
func (fakeNetworkSandbox) Restricted() providerutils.SandboxSession {
	return fakeSandbox{}
}

type fakeSandboxProvider struct{}

func (fakeSandboxProvider) SpecificationVersion() string { return "harness-sandbox-v1" }
func (fakeSandboxProvider) ProviderID() string           { return "fake" }
func (fakeSandboxProvider) CreateSession(context.Context, harness.CreateSandboxSessionOptions) (harness.NetworkSandboxSession, error) {
	return fakeNetworkSandbox{}, nil
}
func (fakeSandboxProvider) ResumeSession(context.Context, string) (harness.NetworkSandboxSession, error) {
	return fakeNetworkSandbox{}, nil
}

// ─── fake harness (mirrors pkg/harness/agent_test.go's newMockHarness,
// scoped locally) ──────────────────────────────────────────────────────

type fakeHarnessOptions struct {
	script         func() []harness.StreamPart
	continueScript func() []harness.StreamPart
	// promptDone, when set, holds PromptControl.Done() open until the
	// returned channel closes — a window during which the turn is
	// genuinely still "running" from the driver's perspective (see
	// pkg/harness/agent_test.go's steerTestScript doc). Used to simulate a
	// harness that is still streaming when a time slice's timer fires.
	promptDone    func() <-chan struct{}
	doSuspendTurn func(context.Context) (*harness.ContinueTurnState, error)
}

type fakeSession struct {
	opts fakeHarnessOptions
}

func (s *fakeSession) SessionID() string { return "fake-session-1" }
func (s *fakeSession) IsResume() bool    { return false }

func (s *fakeSession) doTurn(script []harness.StreamPart, emit harness.EmitFunc) (harness.PromptControl, error) {
	pc := &fakePromptControl{done: make(chan struct{})}
	go func() {
		for _, p := range script {
			emit(p)
		}
		if s.opts.promptDone != nil {
			<-s.opts.promptDone()
		}
		close(pc.done)
	}()
	return pc, nil
}

func (s *fakeSession) DoPromptTurn(_ context.Context, o harness.PromptTurnOptions) (harness.PromptControl, error) {
	var script []harness.StreamPart
	if s.opts.script != nil {
		script = s.opts.script()
	}
	return s.doTurn(script, o.Emit)
}

func (s *fakeSession) DoContinueTurn(_ context.Context, o harness.ContinueTurnOptions) (harness.PromptControl, error) {
	var script []harness.StreamPart
	if s.opts.continueScript != nil {
		script = s.opts.continueScript()
	}
	return s.doTurn(script, o.Emit)
}

func (s *fakeSession) DoCompact(context.Context, string) error { return nil }

func (s *fakeSession) DoSuspendTurn(ctx context.Context) (*harness.ContinueTurnState, error) {
	if s.opts.doSuspendTurn != nil {
		return s.opts.doSuspendTurn(ctx)
	}
	return harness.NewContinueTurnState("fake", map[string]any{})
}

func (s *fakeSession) DoDetach(context.Context) (*harness.ResumeSessionState, error) {
	return harness.NewResumeSessionState("fake", map[string]any{})
}
func (s *fakeSession) DoStop(context.Context) (*harness.ResumeSessionState, error) {
	return harness.NewResumeSessionState("fake", map[string]any{})
}
func (s *fakeSession) DoDestroy(context.Context) error { return nil }

type fakePromptControl struct {
	done chan struct{}
}

func (c *fakePromptControl) SubmitToolResult(context.Context, harness.ToolResultSubmission) error {
	return nil
}
func (c *fakePromptControl) SubmitToolApproval(context.Context, harness.ToolApprovalSubmission) error {
	return nil
}
func (c *fakePromptControl) Done() <-chan struct{} { return c.done }
func (c *fakePromptControl) Err() error            { return nil }

type fakeHarnessAdapter struct {
	session *fakeSession
}

func (h *fakeHarnessAdapter) SpecificationVersion() string { return harness.SpecificationVersion }
func (h *fakeHarnessAdapter) HarnessID() string            { return "fake" }
func (h *fakeHarnessAdapter) BuiltinTools() map[string]harness.BuiltinTool {
	return map[string]harness.BuiltinTool{}
}
func (h *fakeHarnessAdapter) DoStart(context.Context, harness.StartOptions) (harness.Session, error) {
	return h.session, nil
}

// newFakeHarnessAgent builds a *harness.Agent over a fresh fakeSession,
// wired to fakeSandboxProvider so RunHarnessAgent's internal CreateSession
// (which never passes a per-call SandboxSession) has a sandbox to acquire.
func newFakeHarnessAgent(t *testing.T, opts fakeHarnessOptions, agentOpts ...func(*harness.AgentSettings)) *harness.Agent {
	t.Helper()
	session := &fakeSession{opts: opts}
	settings := harness.AgentSettings{Harness: &fakeHarnessAdapter{session: session}, Sandbox: fakeSandboxProvider{}}
	for _, f := range agentOpts {
		f(&settings)
	}
	a, err := harness.NewAgent(settings)
	if err != nil {
		t.Fatalf("harness.NewAgent: %v", err)
	}
	return a
}

func withStopWhenStepCount(n int) func(*harness.AgentSettings) {
	return func(s *harness.AgentSettings) {
		s.StopWhen = append(s.StopWhen, ai.StepCountIs(n))
	}
}

// ─── collecting writer ─────────────────────────────────────────────────

type collectingWriter struct {
	mu     sync.Mutex
	chunks []ai.UIMessageChunk
	closed bool
}

func (w *collectingWriter) Write(chunk ai.UIMessageChunk) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.chunks = append(w.chunks, chunk)
	return nil
}
func (w *collectingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}
func (w *collectingWriter) types() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.chunks))
	for i, c := range w.chunks {
		typ, _ := c["type"].(string)
		out[i] = typ
	}
	return out
}
func (w *collectingWriter) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

func usageParts(input, output int) harness.Usage {
	i, o := input, output
	return harness.Usage{InputTokens: harness.InputTokenUsage{Total: &i}, OutputTokens: harness.OutputTokenUsage{Total: &o}}
}

// ─── tests ──────────────────────────────────────────────────────────────

// TestRunHarnessAgentTimeSlice_FinishesFirstTurn ports TS "first turn
// finishes: streams chunks, writes one terminal finish, keeps the session
// warm" (run-harness-agent-slice.test.ts).
func TestRunHarnessAgentTimeSlice_FinishesFirstTurn(t *testing.T) {
	a := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.TextStartPart{ID: "t1"},
				&harness.TextDeltaPart{ID: "t1", Delta: "done"},
				&harness.TextEndPart{ID: "t1"},
				&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, Usage: usageParts(11, 7)},
				&harness.FinishPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, TotalUsage: usageParts(11, 7)},
			}
		},
	})
	writer := &collectingWriter{}

	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("hi"), SessionID: "ses_1"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: a, State: state, Writable: writer,
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentTimeSlice: %v", err)
	}
	if next.Status != HarnessWorkflowStatusFinished {
		t.Fatalf("Status = %v, want finished", next.Status)
	}
	if !writer.isClosed() {
		t.Fatal("writable should be closed on a finished turn")
	}
	if next.FinalResult == nil || next.FinalResult.FinishReason != "stop" {
		t.Fatalf("FinalResult = %+v", next.FinalResult)
	}
	if next.FinalResult.Usage == nil || *next.FinalResult.Usage.InputTokens != 11 || *next.FinalResult.Usage.OutputTokens != 7 {
		t.Fatalf("Usage = %+v", next.FinalResult.Usage)
	}
	if next.ResumeFrom == nil {
		t.Fatal("ResumeFrom should be populated (session detached, not destroyed)")
	}
	got := writer.types()
	// Unlike the TS test (which fakes toUIMessageStream() directly with a
	// canned chunk list), this test drives the real
	// *ai.StreamTextResult.ToUIMessageStream() pipeline end to end, so the
	// step-boundary chunks it naturally emits (pkg/ai/ui_message_stream.go,
	// outside this package's scope) are part of the expected output too.
	want := []string{"start", "text-start", "text-delta", "text-end", "finish-step", "finish-step", "finish"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("chunk types = %v, want %v", got, want)
	}
}

// TestRunHarnessAgentTimeSlice_DestroyOnFinish ports TS "destroyOnFinish
// destroys the sandbox and drops resume state".
func TestRunHarnessAgentTimeSlice_DestroyOnFinish(t *testing.T) {
	a := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, Usage: usageParts(1, 1)},
				&harness.FinishPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, TotalUsage: usageParts(1, 1)},
			}
		},
	})
	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("hi"), SessionID: "ses_1"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: a, State: state, DestroyOnFinish: true, Writable: &collectingWriter{},
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentTimeSlice: %v", err)
	}
	if next.Status != HarnessWorkflowStatusFinished {
		t.Fatalf("Status = %v, want finished", next.Status)
	}
	if next.ResumeFrom != nil {
		t.Fatalf("ResumeFrom = %+v, want nil when DestroyOnFinish", next.ResumeFrom)
	}
}

// TestRunHarnessAgentTimeSlice_ToolApprovalPause ports TS "tool approval
// pause suspends the turn and closes the response stream".
func TestRunHarnessAgentTimeSlice_ToolApprovalPause(t *testing.T) {
	a := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: "{}"},
				&harness.ToolApprovalRequestPart{ApprovalID: "a1", ToolCallID: "c1"},
			}
		},
		doSuspendTurn: func(context.Context) (*harness.ContinueTurnState, error) {
			return &harness.ContinueTurnState{
				Type: harness.LifecycleStateContinueTurn, HarnessID: "fake", SpecificationVersion: harness.SpecificationVersion,
				PendingToolApprovals: []harness.PendingToolApproval{{ApprovalID: "a1", ToolCallID: "c1", ToolName: "weather", Kind: harness.PendingToolApprovalBuiltin}},
			}, nil
		},
	})
	writer := &collectingWriter{}

	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("hi"), SessionID: "ses_1"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: a, State: state, Writable: writer,
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentTimeSlice: %v", err)
	}
	if next.Status != HarnessWorkflowStatusAwaitingToolApproval {
		t.Fatalf("Status = %v, want awaiting_tool_approval", next.Status)
	}
	if next.ContinueFrom == nil || len(next.ContinueFrom.PendingToolApprovals) != 1 {
		t.Fatalf("ContinueFrom = %+v", next.ContinueFrom)
	}
	if next.ResumeFrom == nil || next.ResumeFrom.ContinueFrom != next.ContinueFrom {
		t.Fatalf("ResumeFrom should wrap ContinueFrom, got %+v", next.ResumeFrom)
	}
	if !writer.isClosed() {
		t.Fatal("writable should be closed once the terminal finish for this UI message is written")
	}
}

// TestRunHarnessAgentTimeSlice_SuspendsAtBudgetAndContinues ports TS
// "completes the time slice: suspends at the budget and carries the cursor
// forward" plus "continued slice reopens active parts and preserves
// aggregate token usage": a slow first slice leaves a text and reasoning
// part open, gets cut off by the time-slice timer, and the continued second
// slice reopens both parts (writing their `-start` chunks again during
// harness_test's own reconstruction, though the raw chunk log below only
// carries the delta/end pair — the -start replay is a UI-message-stream
// reconstruction concern on the reader side, not this writer's).
func TestRunHarnessAgentTimeSlice_SuspendsAtBudgetAndContinues(t *testing.T) {
	suspendReached := make(chan struct{})
	blockUntil := make(chan struct{})
	first := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.TextStartPart{ID: "t1"},
				&harness.TextDeltaPart{ID: "t1", Delta: "first"},
				&harness.ReasoningStartPart{ID: "r1"},
				&harness.ReasoningDeltaPart{ID: "r1", Delta: "think"},
			}
		},
		promptDone: func() <-chan struct{} { return blockUntil },
		doSuspendTurn: func(context.Context) (*harness.ContinueTurnState, error) {
			close(suspendReached)
			close(blockUntil)
			return harness.NewContinueTurnState("fake", map[string]any{"tag": "suspended"})
		},
	})
	firstWriter := &collectingWriter{}

	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("hi"), SessionID: "ses_1"})
	firstState, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: first, State: state, TimeSliceSeconds: 0.05, Writable: firstWriter,
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentTimeSlice (first slice): %v", err)
	}
	select {
	case <-suspendReached:
	default:
		t.Fatal("expected the timer to have fired DoSuspendTurn")
	}
	if firstState.Status != HarnessWorkflowStatusReadyForNextStep {
		t.Fatalf("Status = %v, want ready_for_next_step", firstState.Status)
	}
	if firstWriter.isClosed() {
		t.Fatal("a suspended slice must not close the output stream")
	}
	gotFirst := firstWriter.types()
	wantFirst := []string{"start", "text-start", "text-delta", "reasoning-start", "reasoning-delta", "text-end", "reasoning-end"}
	if strings.Join(gotFirst, ",") != strings.Join(wantFirst, ",") {
		t.Fatalf("first slice chunk types = %v, want %v", gotFirst, wantFirst)
	}
	if firstState.StreamContext == nil || firstState.StreamContext.ActiveTextParts["t1"] == nil || firstState.StreamContext.ActiveReasoningParts["r1"] == nil {
		t.Fatalf("StreamContext should carry the still-open text/reasoning parts, got %+v", firstState.StreamContext)
	}

	second := newFakeHarnessAgent(t, fakeHarnessOptions{
		continueScript: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.TextDeltaPart{ID: "t1", Delta: " second"},
				&harness.TextEndPart{ID: "t1"},
				&harness.ReasoningDeltaPart{ID: "r1", Delta: " more"},
				&harness.ReasoningEndPart{ID: "r1"},
				&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, Usage: usageParts(120, 30)},
				&harness.FinishPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, TotalUsage: usageParts(120, 30)},
			}
		},
	})
	secondWriter := &collectingWriter{}

	secondState, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: second, State: firstState, Writable: secondWriter,
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentTimeSlice (second slice): %v", err)
	}
	if secondState.Status != HarnessWorkflowStatusFinished {
		t.Fatalf("Status = %v, want finished", secondState.Status)
	}
	if secondState.FinalResult == nil || secondState.FinalResult.Usage == nil || *secondState.FinalResult.Usage.InputTokens != 120 {
		t.Fatalf("FinalResult.Usage = %+v", secondState.FinalResult)
	}
	gotSecond := secondWriter.types()
	// The opening `start` is dropped on a continued slice (state.ContinueFrom
	// != nil). Unlike TS's unit test (which fakes toUIMessageStream()
	// directly with a canned chunk list starting right at `text-delta`), the
	// continued slice here drives a brand-new *ai.StreamTextResult whose own
	// ToUIMessageStream synthesizes fresh text-start/reasoning-start chunks
	// for the "t1"/"r1" ids the moment their first delta arrives — it has no
	// visibility into the *prior* execution's stream, so this engine's own
	// writeRequiredPrelude replay (harness.go, exercised by
	// TestRunHarnessAgentTimeSlice_ToolApprovalPause's simpler single-slice
	// cases) never gets a chance to fire here; a real orphan-delta-with-no-
	// start from a bridge-backed adapter would.
	wantSecond := []string{"text-start", "text-delta", "text-end", "reasoning-start", "reasoning-delta", "reasoning-end", "finish-step", "finish-step", "finish"}
	if strings.Join(gotSecond, ",") != strings.Join(wantSecond, ",") {
		t.Fatalf("second slice chunk types = %v, want %v", gotSecond, wantSecond)
	}
}

// TestRunHarnessAgentStep_ReturnsReadyForNextStepAtStepBoundary mirrors TS
// runHarnessAgentStep's "returns ready_for_next_step when a semantic step
// ends before the turn": a StopWhen-configured agent suspends the driver's
// own turn at its first step boundary without ever observing further parts
// the (real) adapter would have kept streaming.
func TestRunHarnessAgentStep_ReturnsReadyForNextStepAtStepBoundary(t *testing.T) {
	a := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.TextStartPart{ID: "t1"},
				&harness.TextDeltaPart{ID: "t1", Delta: "working"},
				&harness.TextEndPart{ID: "t1"},
				&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonToolCalls}, Usage: usageParts(1, 1)},
				// Only reached if the driver fails to stop at the step
				// boundary — must never be observed.
				&harness.TextDeltaPart{ID: "t2", Delta: "must not appear"},
			}
		},
	}, withStopWhenStepCount(1))
	writer := &collectingWriter{}

	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("hi"), SessionID: "ses_1"})
	next, err := RunHarnessAgentStep(context.Background(), RunHarnessAgentStepOptions{
		Agent: a, State: state, Writable: writer,
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentStep: %v", err)
	}
	if next.Status != HarnessWorkflowStatusReadyForNextStep {
		t.Fatalf("Status = %v, want ready_for_next_step", next.Status)
	}
	if next.ContinueFrom == nil {
		t.Fatal("ContinueFrom should be populated")
	}
	if writer.isClosed() {
		t.Fatal("a ready_for_next_step slice must not close the output stream")
	}
	got := writer.types()
	// See TestRunHarnessAgentTimeSlice_FinishesFirstTurn's doc comment: the
	// real ToUIMessageStream pipeline emits its own step-boundary chunks.
	want := []string{"start", "text-start", "text-delta", "text-end", "finish-step", "finish-step"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("chunk types = %v, want %v", got, want)
	}
}

// scoreOutput is the structured output type for the output-persistence
// tests. Mirrors TS run-harness-agent-output.test.ts's `ScoreOutput`.
type scoreOutput struct {
	Score float64 `json:"score"`
}

// TestRunHarnessAgentTimeSlice_PersistsValidatedOutput ports TS
// "persists parsed and schema-validated HarnessAgent output"
// (run-harness-agent-output.test.ts): d70a334, the item explicitly deferred
// to WG13.
func TestRunHarnessAgentTimeSlice_PersistsValidatedOutput(t *testing.T) {
	a := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.TextStartPart{ID: "t1"},
				&harness.TextDeltaPart{ID: "t1", Delta: `{"score":3}`},
				&harness.TextEndPart{ID: "t1"},
				&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, Usage: usageParts(1, 1)},
				&harness.FinishPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, TotalUsage: usageParts(1, 1)},
			}
		},
	}, func(s *harness.AgentSettings) {
		s.Output = ai.ObjectOutput[scoreOutput](ai.ObjectOutputOptions{Schema: ai.SchemaFor[scoreOutput]()})
	})
	writer := &collectingWriter{}

	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("Score this."), SessionID: "ses_1"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: a, State: state, Writable: writer,
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentTimeSlice: %v", err)
	}
	if next.Status != HarnessWorkflowStatusFinished {
		t.Fatalf("Status = %v, want finished", next.Status)
	}
	if !writer.isClosed() {
		t.Fatal("writable should be closed on a finished turn")
	}
	if next.FinalResult == nil {
		t.Fatal("FinalResult should be populated")
	}
	out, ok := next.FinalResult.Output.(scoreOutput)
	if !ok {
		t.Fatalf("Output = %#v (%T), want scoreOutput", next.FinalResult.Output, next.FinalResult.Output)
	}
	if out.Score != 3 {
		t.Fatalf("Output.Score = %v, want 3", out.Score)
	}
}

// TestRunHarnessAgentTimeSlice_OutputValidationFailurePreservesResumeState
// ports TS "detaches the session and preserves the output error before
// closing the stream": an output-read failure must not close the writable
// (the terminal `finish` for this UI message is never written), and the
// session must still be detached so the resume state isn't lost.
func TestRunHarnessAgentTimeSlice_OutputValidationFailurePreservesResumeState(t *testing.T) {
	a := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.TextStartPart{ID: "t1"},
				&harness.TextDeltaPart{ID: "t1", Delta: "not json"},
				&harness.TextEndPart{ID: "t1"},
				&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, Usage: usageParts(1, 1)},
				&harness.FinishPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, TotalUsage: usageParts(1, 1)},
			}
		},
	}, func(s *harness.AgentSettings) {
		s.Output = ai.ObjectOutput[scoreOutput](ai.ObjectOutputOptions{Schema: ai.SchemaFor[scoreOutput]()})
	})
	writer := &collectingWriter{}

	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("Score this."), SessionID: "ses_1"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: a, State: state, Writable: writer,
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentTimeSlice: %v", err)
	}
	if next.Status != HarnessWorkflowStatusFailed {
		t.Fatalf("Status = %v, want failed", next.Status)
	}
	if next.Error == "" {
		t.Fatal("Error should be populated")
	}
	if next.FinalResult != nil {
		t.Fatalf("FinalResult = %+v, want nil", next.FinalResult)
	}
	if next.ResumeFrom == nil {
		t.Fatal("ResumeFrom should still be populated (session detached, not destroyed)")
	}
	if writer.isClosed() {
		t.Fatal("writable must not be closed: the terminal finish for this UI message is never written on an output failure")
	}
	if types := writer.types(); len(types) > 0 && types[len(types)-1] == "finish" {
		t.Fatalf("last chunk = %q, must not be a terminal finish", types[len(types)-1])
	}
}

// TestRunHarnessAgentTimeSlice_NoOutputCapabilitySkipsOutput ports TS
// "finishes a text-only HarnessAgent turn without reading output": an agent
// with no configured Output never populates FinalResult.Output.
func TestRunHarnessAgentTimeSlice_NoOutputCapabilitySkipsOutput(t *testing.T) {
	a := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{
				&harness.StreamStartPart{},
				&harness.TextStartPart{ID: "t1"},
				&harness.TextDeltaPart{ID: "t1", Delta: "Hello."},
				&harness.TextEndPart{ID: "t1"},
				&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, Usage: usageParts(1, 1)},
				&harness.FinishPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, TotalUsage: usageParts(1, 1)},
			}
		},
	})
	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("Say hello."), SessionID: "ses_1"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: a, State: state, Writable: &collectingWriter{},
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentTimeSlice: %v", err)
	}
	if next.Status != HarnessWorkflowStatusFinished {
		t.Fatalf("Status = %v, want finished", next.Status)
	}
	if next.FinalResult == nil || next.FinalResult.Output != nil {
		t.Fatalf("FinalResult.Output = %#v, want nil", next.FinalResult)
	}
}

// TestRunHarnessAgentSlice_MapsReadyForNextStepToTimedOut ports TS "supports
// sliceTimeoutSeconds and maps ready_for_next_step to timed_out".
func TestRunHarnessAgentSlice_MapsReadyForNextStepToTimedOut(t *testing.T) {
	blockUntil := make(chan struct{})
	a := newFakeHarnessAgent(t, fakeHarnessOptions{
		script: func() []harness.StreamPart {
			return []harness.StreamPart{&harness.StreamStartPart{}}
		},
		promptDone: func() <-chan struct{} { return blockUntil },
		doSuspendTurn: func(context.Context) (*harness.ContinueTurnState, error) {
			close(blockUntil)
			return harness.NewContinueTurnState("fake", map[string]any{})
		},
	})
	writer := &collectingWriter{}

	state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("hi"), SessionID: "ses_1"})
	//nolint:staticcheck // exercising the deprecated wrapper deliberately
	next, err := RunHarnessAgentSlice(context.Background(), RunHarnessAgentSliceOptions{
		Agent: a, State: state, SliceTimeoutSeconds: 0.05, Writable: writer,
	})
	if err != nil {
		t.Fatalf("RunHarnessAgentSlice: %v", err)
	}
	if next.Status != HarnessWorkflowStatusTimedOut {
		t.Fatalf("Status = %v, want timed_out", next.Status)
	}
}

// TestFinalizeHarnessWorkflow exercises FinalizeHarnessWorkflow's three
// branches (failed/finished/best-effort). Mirrors TS
// `finalizeHarnessWorkflow`.
func TestFinalizeHarnessWorkflow(t *testing.T) {
	if _, err := FinalizeHarnessWorkflow(HarnessWorkflowState{Status: HarnessWorkflowStatusFailed, Error: "boom"}); err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want \"boom\"", err)
	}
	final := HarnessWorkflowFinalResult{SessionID: "s1", FinishReason: "stop"}
	got, err := FinalizeHarnessWorkflow(HarnessWorkflowState{Status: HarnessWorkflowStatusFinished, FinalResult: &final})
	if err != nil || got != final {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
	got, err = FinalizeHarnessWorkflow(HarnessWorkflowState{SessionID: "s2", Status: HarnessWorkflowStatusNotStarted})
	if err != nil || got.SessionID != "s2" || got.FinishReason != "unknown" {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
}

func TestChanHarnessWorkflowWriter(t *testing.T) {
	ch := make(chan ai.UIMessageChunk, 4)
	w := NewChanHarnessWorkflowWriter(ch)
	if err := w.Write(ai.UIMessageChunk{"type": "start"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := <-ch
	if got["type"] != "start" {
		t.Fatalf("got = %v", got)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after Close")
	}
}

// TestWriteWorkflowChunk_ReplaysCarriedOverPartOnce is a whitebox unit test
// of writeWorkflowChunk/writeRequiredPrelude/recordWorkflowChunk against a
// text part carried over from a prior execution's StreamContext (the shape a
// bridge-backed adapter's genuine cross-process resume produces: a delta for
// an already-open part with no re-announced `text-start`). Ports TS's
// "continued slice reconstructs a partial tool input without creating a
// duplicate UI part" scenario at the buffering-logic level, since driving it
// end to end (TestRunHarnessAgentTimeSlice_SuspendsAtBudgetAndContinues)
// exercises a different code path — see that test's doc comment.
func TestWriteWorkflowChunk_ReplaysCarriedOverPartOnce(t *testing.T) {
	sc := newMutableStreamContext(&HarnessWorkflowStreamContext{
		ActiveTextParts: map[string]ai.UIMessageChunk{"t1": {"type": "text-start", "id": "t1"}},
	})
	ps := newExecutionPartState()
	writer := &collectingWriter{}

	// First delta for "t1": no text-start chunk precedes it in this
	// execution's stream, so the carried-over one must be replayed first.
	if err := writeWorkflowChunk(writer, ai.UIMessageChunk{"type": "text-delta", "id": "t1", "delta": "a"}, sc, ps); err != nil {
		t.Fatalf("writeWorkflowChunk: %v", err)
	}
	// Second delta for the same id: the prelude must NOT replay again.
	if err := writeWorkflowChunk(writer, ai.UIMessageChunk{"type": "text-delta", "id": "t1", "delta": "b"}, sc, ps); err != nil {
		t.Fatalf("writeWorkflowChunk: %v", err)
	}
	if err := writeWorkflowChunk(writer, ai.UIMessageChunk{"type": "text-end", "id": "t1"}, sc, ps); err != nil {
		t.Fatalf("writeWorkflowChunk: %v", err)
	}

	got := writer.types()
	want := []string{"text-start", "text-delta", "text-delta", "text-end"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("chunk types = %v, want %v", got, want)
	}
	if _, stillActive := sc.activeTextParts["t1"]; stillActive {
		t.Fatal("text-end should have cleared the active part")
	}
}

// TestCloseOpenExecutionParts closes every part this execution opened but
// never closed (a suspended slice's still-streaming text/reasoning), and
// leaves already-closed or never-carried parts alone.
func TestCloseOpenExecutionParts(t *testing.T) {
	sc := newMutableStreamContext(nil)
	sc.activeTextParts["t1"] = ai.UIMessageChunk{"type": "text-start", "id": "t1"}
	sc.activeReasoningParts["r1"] = ai.UIMessageChunk{"type": "reasoning-start", "id": "r1"}
	ps := newExecutionPartState()
	ps.openedTextParts["t1"] = struct{}{}
	ps.openedReasoningParts["r1"] = struct{}{}
	writer := &collectingWriter{}

	if err := closeOpenExecutionParts(writer, sc, ps); err != nil {
		t.Fatalf("closeOpenExecutionParts: %v", err)
	}
	got := writer.types()
	want := []string{"text-end", "reasoning-end"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("chunk types = %v, want %v", got, want)
	}
	if len(ps.openedTextParts) != 0 || len(ps.openedReasoningParts) != 0 {
		t.Fatalf("executionPartState should be cleared, got %+v", ps)
	}
}
