package harness

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// erroringHarness builds a harness whose turn emits parts and then settles its
// control with err (no `error` part, no `finish`), like an adapter that only
// rejects `done`.
func erroringHarness(parts []StreamPart, err error) *mockHarnessResult {
	res := &mockHarnessResult{}
	sess := &mockSession{id: "r22-session"}
	sess.doPromptTurn = func(ctx context.Context, o PromptTurnOptions) (PromptControl, error) {
		base := &mockPromptControl{toolResults: &res.toolResults, toolApprovals: &res.toolApprovals, done: make(chan struct{}), err: err}
		go func() {
			for _, p := range parts {
				o.Emit(p)
			}
			close(base.done)
		}()
		return base, nil
	}
	res.session = sess
	res.harness = &mockHarnessAdapter{id: "mock", session: sess}
	return res
}

func captureTurnLogs(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	turnErrorWrite = func(line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	}
	t.Cleanup(func() { turnErrorWrite = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), lines...)
	}
}

// TS toHarnessStream: a rejected control.done becomes an `error` part, so the
// turn fails with the adapter's own (typed) error and logs "harness stream
// error".
func TestRunPrompt_ControlErrSurfacesAsTypedError(t *testing.T) {
	logs := captureTurnLogs(t)
	want := NewCapabilityUnsupportedError("no thanks", "mock", nil)
	mock := erroringHarness([]StreamPart{&StreamStartPart{}}, want)
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Err()
	if got == nil || !IsCapabilityUnsupportedError(got) {
		t.Fatalf("Err() = %v, want the adapter's CapabilityUnsupportedError", got)
	}
	var found bool
	for _, l := range logs() {
		if strings.HasPrefix(l, "[harness:mock:error session=r22-session] harness stream error: ") && strings.Contains(l, "no thanks") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing 'harness stream error' log: %q", logs())
	}
}

// A turn the caller cancelled is not diagnosed on stderr.
func TestRunPrompt_CancelledTurnIsNotLogged(t *testing.T) {
	logs := captureTurnLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	mock := erroringHarness([]StreamPart{&StreamStartPart{}}, context.Canceled)
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	cancel()
	res, err := a.Stream(ctx, agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err == nil {
		_ = res.Err()
	}
	if got := logs(); len(got) != 0 {
		t.Fatalf("cancelled turn logged: %q", got)
	}
}

// A failure to start the turn is logged ("failed to start harness turn").
func TestRunPrompt_StartFailureIsLogged(t *testing.T) {
	logs := captureTurnLogs(t)
	res := &mockHarnessResult{}
	sess := &mockSession{id: "r22-session"}
	boom := errors.New("boom")
	sess.doPromptTurn = func(context.Context, PromptTurnOptions) (PromptControl, error) { return nil, boom }
	res.session = sess
	res.harness = &mockHarnessAdapter{id: "mock", session: sess}
	a, session := newTestAgent(t, res, nil, Callbacks{}, nil)
	out, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err == nil {
		_ = out.Err()
	}
	var found bool
	for _, l := range logs() {
		if l == "[harness:mock:error session=r22-session] failed to start harness turn: boom\n" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing start-failure log: %q", logs())
	}
}

// TS stream-start `modelId` (the runtime-resolved model) reaches the steps,
// the response and the callbacks; the response id is `${callId}-${step}`.
func TestRunPrompt_StreamStartModelIDReachesStepsAndCallbacks(t *testing.T) {
	var mu sync.Mutex
	var startModel string
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{ModelID: "resolved-model"},
				&TextStartPart{ID: "t1"},
				&TextDeltaPart{ID: "t1", Delta: "hi"},
				&TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{
		OnStart: func(_ context.Context, e ai.OnStartEvent) {
			mu.Lock()
			startModel = e.ModelID
			mu.Unlock()
		},
	}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	steps := res.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	if steps[0].Model.ModelID != "resolved-model" || steps[0].Response.ModelID != "resolved-model" {
		t.Fatalf("step model = %q, response model = %q", steps[0].Model.ModelID, steps[0].Response.ModelID)
	}
	if want := steps[0].CallID + "-0"; steps[0].Response.ID != want {
		t.Fatalf("response id = %q, want %q", steps[0].Response.ID, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if startModel != "resolved-model" {
		t.Fatalf("OnStart model = %q", startModel)
	}
}

// TS maybeExecuteHostTool submits inside its try block, so a toModelOutput the
// harness cannot deliver becomes the tool's error (reported to the runtime as
// an error result) instead of failing the turn.
func TestRunPrompt_UnsupportedToModelOutputIsToolError(t *testing.T) {
	pdf := types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{Data: []byte("x"), MediaType: "application/pdf"},
	}}
	inspect := types.Tool{
		Name: "inspect", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
		ToModelOutput: func(ctx context.Context, o types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
			return &pdf, nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "call", ToolName: "inspect", Input: "{}"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	tools := map[string]types.Tool{"inspect": inspect}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("x"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
	})
	drainRunPrompt(t, out)
	if err := out.Result.Err(); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if len(mock.toolResults) != 1 || !mock.toolResults[0].IsError {
		t.Fatalf("submissions = %+v, want one error result", mock.toolResults)
	}
	if m, _ := mock.toolResults[0].Output.(map[string]interface{}); m == nil || !strings.Contains(m["error"].(string), "Harnesses support only text and inline images") {
		t.Fatalf("error output = %+v", mock.toolResults[0].Output)
	}
}

// TS `_resolveLifecycleCallbacks`: a fresh prompt runs the agent's callback and
// the call's own (including the deprecated `onFinish`-style aliases).
func TestAgent_PerCallCallbacksAreMergedWithSettings(t *testing.T) {
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t"}, &TextDeltaPart{ID: "t", Delta: "x"}, &TextEndPart{ID: "t"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{
		OnStart:   func(context.Context, ai.OnStartEvent) { note("settings.start") },
		OnStepEnd: func(context.Context, ai.OnStepFinishEvent) { note("settings.stepEnd") },
		OnEnd:     func(context.Context, ai.OnFinishEvent) { note("settings.end") },
	}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{
			Prompt: "go", HarnessSession: session,
			OnStart:      func(context.Context, ai.OnStartEvent) { note("call.start") },
			OnStepFinish: func(context.Context, ai.OnStepFinishEvent) { note("call.stepFinish") }, //nolint:staticcheck // exercises the deprecated alias
			OnFinish:     func(context.Context, ai.OnFinishEvent) { note("call.finish") },         //nolint:staticcheck // exercises the deprecated alias
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(order, ",")
	for _, want := range []string{"settings.start,call.start", "settings.stepEnd,call.stepFinish"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("callbacks = %s, missing %q", joined, want)
		}
	}
}

// TS HarnessGenerateTextResult: `usage` is the total across steps (the
// bridge's own total when reported), not the final step's usage.
func TestAgent_GenerateResultUsageIsTheTotal(t *testing.T) {
	toolCalls := FinishReason{Unified: FinishReasonToolCalls}
	stop := FinishReason{Unified: FinishReasonStop}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "a"}, &TextDeltaPart{ID: "a", Delta: "one"}, &TextEndPart{ID: "a"},
				&FinishStepPart{FinishReason: toolCalls, Usage: stringUsage(1, 1)},
				&TextStartPart{ID: "b"}, &TextDeltaPart{ID: "b", Delta: "two"}, &TextEndPart{ID: "b"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(2, 3)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(3, 4)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	result, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session})
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 7 {
		t.Fatalf("Usage.TotalTokens = %v, want the turn total 7", result.Usage.TotalTokens)
	}
	if result.FinalStep.Usage.TotalTokens == nil || *result.FinalStep.Usage.TotalTokens != 5 {
		t.Fatalf("final step usage = %v", result.FinalStep.Usage.TotalTokens)
	}
	if result.RawFinishReason != result.FinalStep.RawFinishReason || result.Response.ID != result.FinalStep.Response.ID { //nolint:staticcheck // checks the deprecated mirror
		t.Fatalf("result does not mirror the final step: %+v", result)
	}
}

// TS reports the result's own StepResult to onStepEnd, and onEnd carries the
// flattened content, response messages and final step.
func TestRunPrompt_StepEndAndEndCallbacksCarryTheFullStep(t *testing.T) {
	var mu sync.Mutex
	var stepEvents []ai.OnStepFinishEvent
	var endEvent ai.OnFinishEvent
	var ended bool
	toolCalls := FinishReason{Unified: FinishReasonToolCalls}
	stop := FinishReason{Unified: FinishReasonStop}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{ModelID: "resolved", Warnings: []CallWarning{{Type: "other", Message: "careful"}}},
				&ReasoningStartPart{ID: "r"}, &ReasoningDeltaPart{ID: "r", Delta: "thinking"}, &ReasoningEndPart{ID: "r"},
				&TextStartPart{ID: "a"}, &TextDeltaPart{ID: "a", Delta: "one"}, &TextEndPart{ID: "a"},
				&ToolCallPart{ToolCallID: "c1", ToolName: "Bash", Input: `{"cmd":"ls"}`, ProviderExecuted: true},
				&ToolResultPart{ToolCallID: "c1", ToolName: "Bash", Result: "ok"},
				&FinishStepPart{FinishReason: toolCalls, Usage: stringUsage(1, 1)},
				&TextStartPart{ID: "b"}, &TextDeltaPart{ID: "b", Delta: "two"}, &TextEndPart{ID: "b"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(2, 3)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(3, 4)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{
		OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) {
			mu.Lock()
			stepEvents = append(stepEvents, e)
			mu.Unlock()
		},
		OnEnd: func(_ context.Context, e ai.OnFinishEvent) {
			mu.Lock()
			endEvent, ended = e, true
			mu.Unlock()
		},
	}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(stepEvents) != 2 {
		t.Fatalf("step events = %d", len(stepEvents))
	}
	first := stepEvents[0]
	if len(first.Reasoning) != 1 || first.ReasoningText != "thinking" {
		t.Fatalf("reasoning = %+v / %q", first.Reasoning, first.ReasoningText)
	}
	if len(first.Warnings) != 1 || len(first.ToolCalls) != 1 || len(first.StaticToolCalls)+len(first.DynamicToolCalls) != 1 {
		t.Fatalf("warnings=%v static=%v dynamic=%v calls=%v", first.Warnings, first.StaticToolCalls, first.DynamicToolCalls, first.ToolCalls)
	}
	if first.Response.ID != first.CallID+"-0" || first.Response.ModelID != "resolved" || len(first.Response.Messages) == 0 {
		t.Fatalf("response = %+v", first.Response)
	}
	if !ended || len(endEvent.Content) == 0 || endEvent.FinalStep.Text != "two" || len(endEvent.ResponseMessages) == 0 {
		t.Fatalf("end event = %+v", endEvent)
	}
	if endEvent.Response.ID != endEvent.CallID+"-1" || endEvent.Model.ModelID != "resolved" {
		t.Fatalf("end response/model = %+v / %+v", endEvent.Response, endEvent.Model)
	}
}

// TS wraps the trace tree write in try/catch: a throwing sink never breaks the turn.
func TestTraceTreeReporter_PanickingWriteDoesNotEscape(t *testing.T) {
	r := NewTraceTreeReporter(TraceTreeReporterOptions{Write: func(string) { panic("sink broke") }})
	ctx := context.Background()
	r.OnStart(ctx, telemetry.TelemetryStartEvent{CallID: "c"})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "c"})
}
