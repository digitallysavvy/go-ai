package harness

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Ports the harness.md WG4 turn-telemetry item (TS turn-telemetry.ts,
// 87b4858/a9a22e1/59a2306/b7aa06a): a HarnessAgent turn dispatches through
// pkg/telemetry the same way pkg/ai/generate.go and stream.go do — core
// fires telemetry.Fire* events, and whatever telemetry.TelemetryIntegration
// is registered (or none) decides whether/how spans get created. These
// tests mirror pkg/ai's own "no span without an integration" / "exactly the
// expected spans with one" pattern (see pkg/ai/telemetry_dispatch_test.go).

func spansNamed(rec *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

// TestAgent_TelemetryNoSpanWithoutIntegration verifies pkg/harness never
// creates an OTel span directly (the "core fires events, integrations own
// the spans" contract): with only telemetry.NoopTelemetryIntegration
// registered, a completed turn produces zero spans.
func TestAgent_TelemetryNoSpanWithoutIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "Hello."},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness,
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NoopTelemetryIntegration{}},
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

	if got := rec.Ended(); len(got) != 0 {
		names := make([]string, len(got))
		for i, s := range got {
			names[i] = s.Name()
		}
		t.Fatalf("spans = %v, want none (no integration should create spans directly)", names)
	}
}

// TestAgent_TelemetrySpanNesting ports the turn/step/model-call/tool span
// nesting TS turn-telemetry.ts establishes: with telemetry.NewOpenTelemetry
// registered, a one-step turn with a single host tool call produces exactly
// one "ai.harness harness-model" turn span, one "step 1" span nested under it, one "chat"
// model-call span nested under the step, and one "execute_tool getWeather"
// span (opened by ensureToolExecutionStarted as soon as the host tool's
// execution begins, TS #21696) also nested under the step it ran in.
func TestAgent_TelemetrySpanNesting(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("harness-telemetry-test")

	weatherTool := types.Tool{
		Name: "getWeather", Description: "gets the weather", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "call_1", ToolName: "getWeather", Input: "{}"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				&ToolResultPart{ToolCallID: "call_1", ToolName: "getWeather", Result: "sunny"},
				&TextDeltaPart{ID: "t1", Delta: "It is sunny."},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(4, 4)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness, Model: "harness-model", UserTools: map[string]types.Tool{"getWeather": weatherTool},
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "weather?", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// GenAI root span is "<operation> <modelId>"; ai.harness is unmapped in TS
	// mapOperationName, so it keeps its operation id. Step spans are 1-indexed.
	turnSpans := spansNamed(rec, "ai.harness harness-model")
	if len(turnSpans) != 1 {
		t.Fatalf("ai.harness spans = %d, want 1", len(turnSpans))
	}
	turnSpan := turnSpans[0]

	// Two model steps ran (the tool-call step, then the text-answer step),
	// so two step spans and two chat spans are expected — TS turn-telemetry
	// opens a fresh step/model-call span pair per finish-step boundary.
	step0 := spansNamed(rec, "step 1")
	step1 := spansNamed(rec, "step 2")
	if len(step0) != 1 || len(step1) != 1 {
		t.Fatalf("step spans: step0=%d step1=%d, want 1 each", len(step0), len(step1))
	}
	if step0[0].Parent().SpanID() != turnSpan.SpanContext().SpanID() {
		t.Fatalf("step 0 span is not a child of the ai.harness turn span")
	}
	if step1[0].Parent().SpanID() != turnSpan.SpanContext().SpanID() {
		t.Fatalf("step 1 span is not a child of the ai.harness turn span")
	}

	chatSpans := spansNamed(rec, "chat harness-model")
	if len(chatSpans) != 2 {
		t.Fatalf("chat spans = %d, want 2", len(chatSpans))
	}
	chatParents := map[string]bool{}
	for _, s := range chatSpans {
		chatParents[s.Parent().SpanID().String()] = true
	}
	if !chatParents[step0[0].SpanContext().SpanID().String()] || !chatParents[step1[0].SpanContext().SpanID().String()] {
		t.Fatalf("chat spans are not each nested under their own step span")
	}

	toolSpans := spansNamed(rec, "execute_tool getWeather")
	if len(toolSpans) != 1 {
		t.Fatalf("execute_tool getWeather spans = %d, want 1", len(toolSpans))
	}
	// TS #21696: toolExecutionStart (and so the execute_tool span) now
	// fires as soon as the host tool's own execution begins — while step 0
	// is still open, since getWeather's ToolCallPart arrives before step
	// 0's `finish-step` — rather than being deferred until its result is
	// observed (which, per the scripted `tool-result` part arriving after
	// step 0's `finish-step`, would be once step 1 has already opened).
	// The execute_tool span is therefore nested under step 0, the step the
	// tool call actually ran in, not step 1 (where its result happened to
	// stream).
	if toolSpans[0].Parent().SpanID() != step0[0].SpanContext().SpanID() {
		t.Fatalf("execute_tool span is not nested under the step it started executing in (step 0)")
	}
}

// slowToolTurnSession is a minimal Session for
// TestAgent_TelemetryToolExecutionStartFiresWhileToolRunning, modeled on
// run_prompt_test.go's concurrentToolsSession: it emits only stream-start +
// the tool-call event upfront, and defers finish-step/finish until
// SubmitToolResult reports the (single) host tool's result — so finish-step
// can never arrive while the tool is still executing, the same real-world
// guarantee a harness with no host tools mid-step would give.
type slowToolTurnSession struct {
	toolName string

	mu   sync.Mutex
	emit EmitFunc
	done chan struct{}
}

func (s *slowToolTurnSession) SessionID() string { return "slow-tool-telemetry" }
func (s *slowToolTurnSession) IsResume() bool    { return false }

func (s *slowToolTurnSession) DoPromptTurn(_ context.Context, opts PromptTurnOptions) (PromptControl, error) {
	s.mu.Lock()
	s.emit = opts.Emit
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	go func() {
		opts.Emit(&StreamStartPart{})
		opts.Emit(&ToolCallPart{ToolCallID: "call_1", ToolName: s.toolName, Input: "{}"})
	}()
	return &slowToolTurnControl{session: s, done: done}, nil
}
func (s *slowToolTurnSession) DoContinueTurn(context.Context, ContinueTurnOptions) (PromptControl, error) {
	return nil, nil
}
func (s *slowToolTurnSession) DoCompact(context.Context, string) error { return nil }
func (s *slowToolTurnSession) DoSuspendTurn(context.Context) (*ContinueTurnState, error) {
	return nil, nil
}
func (s *slowToolTurnSession) DoDetach(context.Context) (*ResumeSessionState, error) {
	return nil, nil
}
func (s *slowToolTurnSession) DoStop(context.Context) (*ResumeSessionState, error) {
	return nil, nil
}
func (s *slowToolTurnSession) DoDestroy(context.Context) error { return nil }

type slowToolTurnControl struct {
	session *slowToolTurnSession
	done    chan struct{}
}

func (c *slowToolTurnControl) SubmitToolResult(_ context.Context, r ToolResultSubmission) error {
	s := c.session
	s.emit(&ToolResultPart{ToolCallID: r.ToolCallID, ToolName: s.toolName, Result: r.Output, IsError: r.IsError})
	s.emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)})
	s.emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, TotalUsage: stringUsage(1, 1)})
	close(c.done)
	return nil
}
func (c *slowToolTurnControl) Done() <-chan struct{} { return c.done }
func (c *slowToolTurnControl) Err() error            { return nil }

// TestAgent_TelemetryToolExecutionStartFiresWhileToolRunning proves TS
// #21696's real-time semantics apply to telemetry too, not just
// ai.Callbacks.OnToolExecutionStart: the "execute_tool" span must already
// be started (recorded by the SpanRecorder's OnStart, not merely OnEnd)
// while a slow host tool is still blocked mid-execution, rather than only
// appearing once the tool finishes and recordToolResult fires both
// telemetry events back-to-back (the pre-fix, TS-divergent behavior this
// test would have caught).
func TestAgent_TelemetryToolExecutionStartFiresWhileToolRunning(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("harness-telemetry-slow-tool-test")

	release := make(chan struct{})
	started := make(chan struct{})
	slowTool := types.Tool{
		Name: "slowTool", Description: "a slow tool", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			close(started)
			<-release
			return "done", nil
		},
	}
	tools := map[string]types.Tool{"slowTool": slowTool}
	session := &slowToolTurnSession{toolName: "slowTool"}

	out := runPrompt(context.Background(), runPromptInput{
		Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
		Prompt: TextPrompt("go slow"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(), Model: "harness-model",
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("slowTool never started executing")
	}

	// The tool is now blocked mid-execution. Its execute_tool span must
	// already have been started (not just ended) — proving the telemetry
	// start event fired in real time rather than being deferred until the
	// tool's outcome is known.
	var sawStartedUnended bool
	deadline := time.After(2 * time.Second)
	for !sawStartedUnended {
		for _, s := range rec.Started() {
			if s.Name() == "execute_tool slowTool" {
				sawStartedUnended = true
				break
			}
		}
		if sawStartedUnended {
			break
		}
		select {
		case <-deadline:
			t.Fatal("execute_tool slowTool span was not started while the tool was still running")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := spansNamed(rec, "execute_tool slowTool"); len(got) != 0 {
		t.Fatalf("execute_tool slowTool should not have ENDED yet (tool still blocked), ended spans = %d", len(got))
	}

	close(release)

	stream := out.Result.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	<-out.Done
	if out.Result.Err() != nil {
		t.Fatalf("turn error: %v", out.Result.Err())
	}

	if got := spansNamed(rec, "execute_tool slowTool"); len(got) != 1 {
		t.Fatalf("execute_tool slowTool ended spans = %d, want 1 once the tool finished", len(got))
	}
}

// TestAgent_TelemetryEndsOnPause verifies the turn's root telemetry span
// still ends (mirroring TS finishForHostInputPause's `lifecycle.end` call)
// even when the turn pauses for a tool approval rather than fully
// finishing — each runPrompt invocation is its own traced operation.
func TestAgent_TelemetryEndsOnPause(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("harness-telemetry-pause-test")

	sensitiveTool := types.Tool{
		Name: "deleteFile", Description: "deletes a file", Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "deleted", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "call_1", ToolName: "deleteFile", Input: "{}"},
			}
		},
	})
	toolApproval := ToolApprovalConfiguration{"deleteFile": ai.ToolApprovalStatusUserApproval}
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness, UserTools: map[string]types.Tool{"deleteFile": sensitiveTool}, ToolApproval: toolApproval,
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	result, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "delete it", HarnessSession: session},
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

	turnSpans := spansNamed(rec, "ai.harness")
	if len(turnSpans) != 1 {
		t.Fatalf("ai.harness spans = %d, want 1 (root span must end even on a pause)", len(turnSpans))
	}
}

// TestAgent_TelemetryWireErrorRecordsSpanStatus verifies a real wire-level
// `error` part (no caller cancellation) fires telError (not telAbort), which
// records an OTel error status on the root span — mirrors
// pkg/ai/generate.go's own error-vs-abort telemetry split.
func TestAgent_TelemetryWireErrorRecordsSpanStatus(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("harness-telemetry-error-test")

	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "partial "},
				&ErrorPart{Error: "boom"},
			}
		},
	})
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness,
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
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
	if err := result.Err(); err == nil {
		t.Fatal("Err() = nil, want the wire-level error")
	}

	turnSpans := spansNamed(rec, "ai.harness")
	if len(turnSpans) != 1 {
		t.Fatalf("ai.harness spans = %d, want 1", len(turnSpans))
	}
	if got := turnSpans[0].Status().Code; got != codes.Error {
		t.Fatalf("ai.harness span status = %v, want codes.Error", got)
	}
}

// TestAgent_TelemetryAbortDoesNotRecordErrorStatus verifies a turn that
// settles as an abort (caller ctx already cancelled — TS 86a84c9's
// contract) fires telAbort instead of telError, so the root span ends
// cleanly rather than with an OTel error status, even though the mock
// harness reports the same wire-level `error` part as the previous test.
func TestAgent_TelemetryAbortDoesNotRecordErrorStatus(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("harness-telemetry-abort-test")

	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "partial "},
				&ErrorPart{Error: "AbortError: This operation was aborted"},
			}
		},
	})
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness,
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := a.Stream(ctx, agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := result.Err(); err == nil {
		t.Fatal("Err() = nil, want a non-nil error")
	}

	turnSpans := spansNamed(rec, "ai.harness")
	if len(turnSpans) != 1 {
		t.Fatalf("ai.harness spans = %d, want 1", len(turnSpans))
	}
	if got := turnSpans[0].Status().Code; got == codes.Error {
		t.Fatalf("ai.harness span status = %v, want anything but codes.Error for an abort", got)
	}
}
