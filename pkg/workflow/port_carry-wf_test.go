package workflow

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func portcarrywfInt(v int64) *int64 { return &v }

// TS 1876747927 (workflow-agent-contract.test.ts 'aggregates stream usage like
// core, keeping token details'): stream totalUsage, and the usage and
// totalUsage of onEnd, keep the token details.
func TestPortcarrywfStreamTotalUsageKeepsTokenDetails(t *testing.T) {
	var mu sync.Mutex
	n := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if call == 1 {
			tc := types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &tc},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{
					InputTokens: portcarrywfInt(3), OutputTokens: portcarrywfInt(2),
					InputDetails:  &types.InputTokenDetails{NoCacheTokens: portcarrywfInt(2), CacheReadTokens: portcarrywfInt(1)},
					OutputDetails: &types.OutputTokenDetails{TextTokens: portcarrywfInt(2)},
				}},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{
				InputTokens: portcarrywfInt(3), OutputTokens: portcarrywfInt(2),
				InputDetails:  &types.InputTokenDetails{NoCacheTokens: portcarrywfInt(2), CacheReadTokens: portcarrywfInt(1)},
				OutputDetails: &types.OutputTokenDetails{TextTokens: portcarrywfInt(2)},
			}},
		}), nil
	}}
	var ends []ai.OnFinishEvent
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	u := res.Usage()
	if u.InputTokens == nil || *u.InputTokens != 6 || u.OutputTokens == nil || *u.OutputTokens != 4 || u.TotalTokens == nil || *u.TotalTokens != 10 {
		t.Fatalf("totalUsage counts = %+v, want 6/4/10", u)
	}
	if d := u.InputDetails; d == nil || *d.NoCacheTokens != 4 || *d.CacheReadTokens != 2 || d.CacheWriteTokens != nil {
		t.Fatalf("totalUsage input details = %+v, want noCache 4, cacheRead 2, cacheWrite unreported", d)
	}
	if d := u.OutputDetails; d == nil || *d.TextTokens != 4 || d.ReasoningTokens != nil {
		t.Fatalf("totalUsage output details = %+v, want text 4, reasoning unreported", d)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("onEnd calls = %d, want 1", len(ends))
	}
	for name, got := range map[string]types.Usage{"usage": ends[0].Usage, "totalUsage": ends[0].TotalUsage} {
		if got.InputDetails == nil || *got.InputDetails.CacheReadTokens != 2 || *got.TotalTokens != 10 {
			t.Fatalf("onEnd %s = %+v, want the stream totalUsage", name, got)
		}
	}
}

// TS 1876747927 ('keeps unknown stream usage counts undefined like core'):
// a count the provider did not report stays nil, not 0.
func TestPortcarrywfStreamTotalUsageKeepsUnknownCountsUnreported(t *testing.T) {
	w, err := NewWorkflowAgent(WorkflowAgent{Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "Done."},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
		}), nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if u := res.Usage(); u.InputTokens != nil || u.OutputTokens != nil || u.TotalTokens != nil {
		t.Fatalf("totalUsage = %+v, want every count unreported", u)
	}
}

func portcarrywfFailingModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, errors.New("model exploded")
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("model exploded")
		},
	}
}

// TS b161730659 ('calls a constructor onError callback like ToolLoopAgent,
// before the call callback'): the constructor callback runs first, for generate
// and stream.
func TestPortcarrywfConstructorOnErrorRunsBeforeCallOnError(t *testing.T) {
	var calls []string
	record := func(label string) ErrorCallback {
		return func(_ context.Context, err error) { calls = append(calls, label+": "+err.Error()) }
	}
	zero := 0
	w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfFailingModel(), MaxRetries: &zero, OnError: record("constructor")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", OnError: record("generate call")}); err == nil {
		t.Fatal("generate: want the model error")
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", OnError: record("stream call")})
	if err == nil {
		_, err = res.ReadAll()
	}
	if err == nil {
		t.Fatal("stream: want the model error")
	}
	want := []string{
		"constructor: model exploded", "generate call: model exploded",
		"constructor: model exploded", "stream call: model exploded",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

// TS b161730659 ('keeps the model error when onError throws'): a panic in
// onError replaces neither the run's error nor its result.
func TestPortcarrywfOnErrorPanicKeepsModelError(t *testing.T) {
	zero := 0
	boom := func(context.Context, error) { panic("onError exploded") }
	w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfFailingModel(), MaxRetries: &zero, OnError: boom})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", OnError: boom}); err == nil || !strings.Contains(err.Error(), "model exploded") {
		t.Fatalf("generate error = %v, want the model error", err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", OnError: boom})
	if err == nil {
		_, err = res.ReadAll()
	}
	if err == nil || !strings.Contains(err.Error(), "model exploded") {
		t.Fatalf("stream error = %v, want the model error", err)
	}
}

// TS b161730659 ('calls constructor and call onAbort callbacks and ignores
// errors they throw'): an already-aborted stream() calls both, constructor
// first, with no steps; the panic of the first changes nothing.
func TestPortcarrywfOnAbortConstructorThenCallPanicIgnored(t *testing.T) {
	var calls []string
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &testutil.MockLanguageModel{},
		OnAbort: func(_ context.Context, steps []types.StepResult) {
			calls = append(calls, "constructor")
			if len(steps) != 0 {
				t.Errorf("constructor steps = %d, want 0", len(steps))
			}
			panic("onAbort exploded")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi", OnAbort: func(context.Context, []types.StepResult) { calls = append(calls, "call") }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
	if len(calls) != 2 || calls[0] != "constructor" || calls[1] != "call" {
		t.Fatalf("calls = %v, want [constructor call]", calls)
	}
}

// ---- scripted model helpers ----

type portcarrywfScript struct {
	mu    sync.Mutex
	steps [][]provider.StreamChunk
	n     int
}

func (s *portcarrywfScript) next() []provider.StreamChunk {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.n
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	s.n++
	return s.steps[i]
}

// portcarrywfModel answers the nth model call with the nth script step, for
// generate and stream alike; the last step repeats.
func portcarrywfModel(steps ...[]provider.StreamChunk) *testutil.MockLanguageModel {
	s := &portcarrywfScript{steps: steps}
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id", ToolSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(s.next()), nil
		},
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			res := &types.GenerateResult{}
			for _, c := range s.next() {
				switch c.Type {
				case provider.ChunkTypeText:
					res.Text += c.Text
				case provider.ChunkTypeToolCall:
					res.ToolCalls = append(res.ToolCalls, *c.ToolCall)
				case provider.ChunkTypeFinish:
					res.FinishReason = c.FinishReason
					if c.Usage != nil {
						res.Usage = *c.Usage
					}
				case provider.ChunkTypeStreamStart:
					res.Warnings = append(res.Warnings, c.Warnings...)
				}
			}
			return res, nil
		},
	}
}

func portcarrywfTextStep(text string) []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: text},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: portcarrywfInt(3), OutputTokens: portcarrywfInt(2)}},
	}
}

func portcarrywfCall(id, name string, args map[string]interface{}) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: name, Arguments: args}}
}

func portcarrywfToolStep(chunks ...provider.StreamChunk) []provider.StreamChunk {
	return append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{InputTokens: portcarrywfInt(3), OutputTokens: portcarrywfInt(2)}})
}

func portcarrywfCitySchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"city"},
	}
}

func portcarrywfWeatherTool(onAvailable func(map[string]interface{})) types.Tool {
	t := types.Tool{
		Name: "weather", Description: "weather", Parameters: portcarrywfCitySchema(),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"temperature": 18}, nil
		},
	}
	if onAvailable != nil {
		t.OnInputAvailable = func(_ context.Context, o types.OnInputAvailableOptions) error {
			onAvailable(o.Input)
			return nil
		}
	}
	return t
}

// runBothModes runs the agent with generate and with stream, each with a fresh
// model, and hands the finished steps to check.
func portcarrywfRunBoth(t *testing.T, cfg func() WorkflowAgent, check func(t *testing.T, mode string, steps []types.StepResult)) {
	t.Helper()
	t.Run("generate", func(t *testing.T) {
		w, err := NewWorkflowAgent(cfg())
		if err != nil {
			t.Fatal(err)
		}
		res, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "weather?"})
		if err != nil {
			t.Fatal(err)
		}
		check(t, "generate", res.Steps)
	})
	t.Run("stream", func(t *testing.T) {
		w, err := NewWorkflowAgent(cfg())
		if err != nil {
			t.Fatal(err)
		}
		res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "weather?"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
		check(t, "stream", res.Steps())
	})
}

// TS 5c0e98478e (workflow-agent-stream-step-model.test.ts 'reports the model
// that produced each step like ToolLoopAgent and generate' and 'reports the
// configured model when the response carries a different model ID').
func TestPortcarrywfStepsReportTheConfiguredModel(t *testing.T) {
	meta := provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "r1", ModelID: "mock-model-id-2026-01-01"}}
	portcarrywfRunBoth(t, func() WorkflowAgent {
		return WorkflowAgent{
			Model: portcarrywfModel(
				append([]provider.StreamChunk{meta}, portcarrywfToolStep(portcarrywfCall("c1", "weather", map[string]interface{}{"city": "London"}))...),
				append([]provider.StreamChunk{meta}, portcarrywfTextStep("It is 18C.")...),
			),
			Tools: []types.Tool{portcarrywfWeatherTool(nil)},
		}
	}, func(t *testing.T, mode string, steps []types.StepResult) {
		if len(steps) != 2 {
			t.Fatalf("%s steps = %d, want 2", mode, len(steps))
		}
		for i, s := range steps {
			if s.Model != (types.StepModel{Provider: "mock-provider", ModelID: "mock-model-id"}) {
				t.Fatalf("%s step %d model = %+v, want mock-provider/mock-model-id", mode, i, s.Model)
			}
		}
	})
}

// portcarrywfStreamCRApplied is false until cr-port-carry-wf-to-stream.md is
// applied to pkg/ai/stream.go (owned by stream); the integrator flips it.
const portcarrywfStreamCRApplied = true

// TS 453d8804a7 (workflow-agent-tool-input-hooks.test.ts): the tool input
// callbacks of stream() match the generic stream loop.
func TestPortcarrywfStreamToolInputAvailableWithoutStreamedInput(t *testing.T) {
	startDelta := func(id string) []provider.StreamChunk {
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeToolInputStart, ID: id, ToolCall: &types.ToolCall{ID: id, ToolName: "weather"}},
			{Type: provider.ChunkTypeToolInputDelta, ID: id, Text: `{"city":`},
			{Type: provider.ChunkTypeToolInputDelta, ID: id, Text: `"London"}`},
			{Type: provider.ChunkTypeToolInputEnd, ID: id},
		}
	}
	london := map[string]interface{}{"city": "London"}
	cases := []struct {
		name string
		step []provider.StreamChunk
		want []string
		// needsCR marks a case that passes only with the stream change of
		// cr-port-carry-wf-to-stream.md (recordDeferredToolInput keeps a tool
		// call that had no tool-input-start).
		needsCR bool
	}{
		{"call without streamed input", portcarrywfToolStep(portcarrywfCall("w", "weather", london)), []string{"available:London"}, true},
		{"streamed input", portcarrywfToolStep(append(startDelta("w"), portcarrywfCall("w", "weather", london))...), []string{"start", "delta", "delta", "available:London"}, false},
		{"several calls without streamed input", portcarrywfToolStep(portcarrywfCall("w1", "weather", london), portcarrywfCall("w2", "weather", map[string]interface{}{"city": "Paris"})), []string{"available:London", "available:Paris"}, true},
		{"input that fails the schema", portcarrywfToolStep(portcarrywfCall("w", "weather", map[string]interface{}{"city": 42.0})), nil, false},
		{"unknown tool", portcarrywfToolStep(portcarrywfCall("m", "missing", london)), nil, false},
		{"provider-executed call", portcarrywfToolStep(
			provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "w", ToolName: "weather", Arguments: london, ProviderExecuted: true}},
			provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "w", ToolName: "weather", Result: map[string]interface{}{"temperature": 18}, ProviderExecuted: true}},
		), []string{"available:London"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.needsCR && !portcarrywfStreamCRApplied {
				t.Skip("needs the pkg/ai/stream.go change of cr-port-carry-wf-to-stream.md (TS 453d8804a7); the integrator sets portcarrywfStreamCRApplied after applying it")
			}
			var mu sync.Mutex
			var got []string
			rec := func(s string) { mu.Lock(); got = append(got, s); mu.Unlock() }
			tool := portcarrywfWeatherTool(func(in map[string]interface{}) { rec("available:" + portcarrywfCity(in)) })
			tool.OnInputStart = func(context.Context, types.OnInputStartOptions) error { rec("start"); return nil }
			tool.OnInputDelta = func(context.Context, types.OnInputDeltaOptions) error { rec("delta"); return nil }
			w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfModel(c.step, portcarrywfTextStep("done")), Tools: []types.Tool{tool}})
			if err != nil {
				t.Fatal(err)
			}
			res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "weather?"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := res.ReadAll(); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(got) != len(c.want) {
				t.Fatalf("callbacks = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("callbacks = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func portcarrywfCity(in map[string]interface{}) string {
	s, _ := in["city"].(string)
	return s
}

// portcarrywfAbortingModel cancels the call's context with reason and then
// fails with the context's cause, the way a provider sees an external abort.
func portcarrywfAbortingModel(cancel context.CancelCauseFunc, reason error) *testutil.MockLanguageModel {
	abortAndFail := func(ctx context.Context) error {
		cancel(reason)
		return context.Cause(ctx)
	}
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, abortAndFail(ctx)
		},
		DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, abortAndFail(ctx)
		},
	}
}

type portcarrywfAbortCalls struct {
	mu                                             sync.Mutex
	ctorErrors, callErrors, ctorAborts, callAborts int
	ctorAbortSteps, callAbortSteps                 []int
	ends                                           int
}

func (c *portcarrywfAbortCalls) cfg(model *testutil.MockLanguageModel) WorkflowAgent {
	zero := 0
	return WorkflowAgent{
		Model: model, MaxRetries: &zero,
		OnError: func(context.Context, error) { c.mu.Lock(); c.ctorErrors++; c.mu.Unlock() },
		OnAbort: func(_ context.Context, steps []types.StepResult) {
			c.mu.Lock()
			c.ctorAborts++
			c.ctorAbortSteps = append(c.ctorAbortSteps, len(steps))
			c.mu.Unlock()
		},
	}
}

// TS ff89832dfc (workflow-agent-abort-reason.test.ts): a generate() aborted
// with a custom reason rejects with that reason, calls onAbort (constructor and
// call), never onError.
func TestPortcarrywfGenerateAbortWithCustomReasonIsAnAbort(t *testing.T) {
	reason := errors.New("user cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	calls := &portcarrywfAbortCalls{}
	w, err := NewWorkflowAgent(calls.cfg(portcarrywfAbortingModel(cancel, reason)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.GenerateWithOptions(ctx, WorkflowGenerateOptions{
		Prompt:  "hi",
		OnError: func(context.Context, error) { calls.mu.Lock(); calls.callErrors++; calls.mu.Unlock() },
		OnAbort: func(_ context.Context, steps []types.StepResult) {
			calls.mu.Lock()
			calls.callAborts++
			calls.callAbortSteps = append(calls.callAbortSteps, len(steps))
			calls.mu.Unlock()
		},
	})
	if !errors.Is(err, reason) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
	calls.mu.Lock()
	defer calls.mu.Unlock()
	if calls.ctorErrors != 0 || calls.callErrors != 0 {
		t.Fatalf("onError calls = %d/%d, want none", calls.ctorErrors, calls.callErrors)
	}
	if calls.ctorAborts != 1 || calls.callAborts != 1 || calls.ctorAbortSteps[0] != 0 || calls.callAbortSteps[0] != 0 {
		t.Fatalf("onAbort calls = %d/%d steps %v/%v, want 1/1 with no steps", calls.ctorAborts, calls.callAborts, calls.ctorAbortSteps, calls.callAbortSteps)
	}
}

// TS ff89832dfc ('reports a generate() abort without a reason as an abort').
func TestPortcarrywfGenerateAbortWithoutReasonIsAnAbort(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	calls := &portcarrywfAbortCalls{}
	w, err := NewWorkflowAgent(calls.cfg(portcarrywfAbortingModel(cancel, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	calls.mu.Lock()
	defer calls.mu.Unlock()
	if calls.ctorErrors != 0 || calls.ctorAborts != 1 {
		t.Fatalf("onError/onAbort = %d/%d, want 0/1", calls.ctorErrors, calls.ctorAborts)
	}
}

// TS ff89832dfc ('reports a model error without an abort as a failure'): a
// provider failure while the call context is live is an error, not an abort.
func TestPortcarrywfGenerateModelErrorIsAFailureNotAnAbort(t *testing.T) {
	providerErr := errors.New("provider unavailable")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	calls := &portcarrywfAbortCalls{}
	zero := 0
	w, err := NewWorkflowAgent(WorkflowAgent{Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return nil, providerErr
	}}, MaxRetries: &zero})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.GenerateWithOptions(ctx, WorkflowGenerateOptions{
		Prompt:  "hi",
		OnError: func(_ context.Context, e error) { calls.mu.Lock(); calls.callErrors++; calls.mu.Unlock(); _ = e },
		OnAbort: func(context.Context, []types.StepResult) { calls.mu.Lock(); calls.callAborts++; calls.mu.Unlock() },
	})
	if !errors.Is(err, providerErr) {
		t.Fatalf("err = %v, want the provider error", err)
	}
	calls.mu.Lock()
	defer calls.mu.Unlock()
	if calls.callErrors != 1 || calls.callAborts != 0 {
		t.Fatalf("onError/onAbort = %d/%d, want 1/0", calls.callErrors, calls.callAborts)
	}
}

// TS ff89832dfc ('agrees with ToolLoopAgent for a stream() abort with a custom
// reason' and 'resolves a stream() abort with a custom reason like an abort
// without a reason'): the stream ends without onError and onEnd, with one
// onAbort, the same with and without a reason.
func TestPortcarrywfStreamAbortWithCustomReasonIsAnAbort(t *testing.T) {
	run := func(reason error) (aborts, errs, ends int, steps int) {
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		calls := &portcarrywfAbortCalls{}
		cfg := calls.cfg(portcarrywfAbortingModel(cancel, reason))
		cfg.OnEnd = func(context.Context, ai.OnFinishEvent) { calls.mu.Lock(); calls.ends++; calls.mu.Unlock() }
		w, err := NewWorkflowAgent(cfg)
		if err != nil {
			t.Fatal(err)
		}
		res, err := w.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi"})
		if err == nil {
			_, _ = res.ReadAll()
			steps = len(res.Steps())
		}
		calls.mu.Lock()
		defer calls.mu.Unlock()
		return calls.ctorAborts, calls.ctorErrors, calls.ends, steps
	}
	for name, reason := range map[string]error{"custom reason": errors.New("user cancelled"), "no reason": nil} {
		aborts, errs, ends, steps := run(reason)
		if aborts != 1 || errs != 0 || ends != 0 || steps != 0 {
			t.Fatalf("%s: onAbort/onError/onEnd/steps = %d/%d/%d/%d, want 1/0/0/0", name, aborts, errs, ends, steps)
		}
	}
}

func portcarrywfContentKinds(parts []types.ContentPart) []string {
	var kinds []string
	for _, p := range parts {
		switch p.(type) {
		case types.TextContent, *types.TextContent:
			kinds = append(kinds, "text")
		case types.ToolCallContent, *types.ToolCallContent:
			kinds = append(kinds, "tool-call")
		case types.ToolResultContent, *types.ToolResultContent:
			kinds = append(kinds, "tool-result")
		case types.ToolErrorContent, *types.ToolErrorContent:
			kinds = append(kinds, "tool-error")
		default:
			kinds = append(kinds, "other")
		}
	}
	return kinds
}

// TS bf1dd2a45d (workflow-agent-stream-invalid-tool-calls.test.ts 'keeps the
// tool call for ...'): stream() keeps an invalid tool call in the step, marked
// invalid, with its tool-error, as generate() does; the next request carries
// one tool message.
func TestPortcarrywfInvalidToolCallsStayInTheStepInBothModes(t *testing.T) {
	cases := []struct {
		name        string
		call        provider.StreamChunk
		wantContent []string
		wantInvalid bool
	}{
		{"an unknown tool", portcarrywfCall("missing-call", "missing", map[string]interface{}{"city": "London"}), []string{"text", "tool-call", "tool-error"}, true},
		{"tool input that fails the schema", portcarrywfCall("weather-call", "weather", map[string]interface{}{"city": 42.0}), []string{"text", "tool-call", "tool-error"}, true},
		{"a valid tool call", portcarrywfCall("weather-call", "weather", map[string]interface{}{"city": "London"}), []string{"text", "tool-call", "tool-result"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var model *testutil.MockLanguageModel
			portcarrywfRunBoth(t, func() WorkflowAgent {
				model = portcarrywfModel(
					portcarrywfToolStep(provider.StreamChunk{Type: provider.ChunkTypeText, Text: "Checking the weather."}, c.call),
					portcarrywfTextStep("It is 18C."),
				)
				return WorkflowAgent{Model: model, Tools: []types.Tool{portcarrywfWeatherTool(nil)}}
			}, func(t *testing.T, mode string, steps []types.StepResult) {
				if len(steps) != 2 {
					t.Fatalf("%s steps = %d, want 2", mode, len(steps))
				}
				if got := portcarrywfContentKinds(steps[0].Content); strings.Join(got, ",") != strings.Join(c.wantContent, ",") {
					t.Fatalf("%s step content = %v, want %v", mode, got, c.wantContent)
				}
				if len(steps[0].ToolCalls) != 1 || steps[0].ToolCalls[0].Invalid != c.wantInvalid {
					t.Fatalf("%s step tool calls = %+v, want one call with invalid=%v", mode, steps[0].ToolCalls, c.wantInvalid)
				}
				calls := model.GenerateCalls
				if mode == "stream" {
					calls = model.StreamCalls
				}
				if len(calls) != 2 {
					t.Fatalf("%s model calls = %d, want 2", mode, len(calls))
				}
				tools := 0
				for _, m := range calls[1].Prompt.Messages {
					if m.Role == types.RoleTool {
						tools++
					}
				}
				if tools != 1 {
					t.Fatalf("%s second request tool messages = %d, want 1", mode, tools)
				}
			})
		})
	}
}

func portcarrywfGeneratorTool(body func(yield func(interface{}, error) bool)) types.Tool {
	tool := portcarrywfWeatherTool(nil)
	tool.Execute = types.StreamingExecutor(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) iter.Seq2[interface{}, error] {
		return body
	})
	return tool
}

func portcarrywfWeatherSteps() *testutil.MockLanguageModel {
	return portcarrywfModel(
		portcarrywfToolStep(portcarrywfCall("weather-call", "weather", map[string]interface{}{"city": "London"})),
		portcarrywfTextStep("It is 18C."),
	)
}

// TS 2473a71288 (workflow-agent-async-generator-tools.test.ts): the last value
// a generator tool yields is the tool result, in generate and stream; a tool
// that fails after yielding is a tool error; a tool that yields nothing has a
// nil output.
func TestPortcarrywfGeneratorToolsUseTheLastYieldedValue(t *testing.T) {
	cases := []struct {
		name     string
		body     func(yield func(interface{}, error) bool)
		wantOut  interface{}
		wantFail bool
	}{
		{"last yielded value", func(yield func(interface{}, error) bool) {
			_ = yield(map[string]interface{}{"status": "loading"}, nil) && yield(map[string]interface{}{"temperature": 18}, nil)
		}, map[string]interface{}{"temperature": 18}, false},
		{"fails after yielding", func(yield func(interface{}, error) bool) {
			if yield(map[string]interface{}{"status": "loading"}, nil) {
				yield(nil, errors.New("boom"))
			}
		}, nil, true},
		{"yields nothing", func(func(interface{}, error) bool) {}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var model *testutil.MockLanguageModel
			portcarrywfRunBoth(t, func() WorkflowAgent {
				model = portcarrywfWeatherSteps()
				return WorkflowAgent{Model: model, Tools: []types.Tool{portcarrywfGeneratorTool(c.body)}}
			}, func(t *testing.T, mode string, steps []types.StepResult) {
				kinds := strings.Join(portcarrywfContentKinds(steps[0].Content), ",")
				if c.wantFail {
					if kinds != "tool-call,tool-error" {
						t.Fatalf("%s step content = %s, want tool-call,tool-error", mode, kinds)
					}
					return
				}
				if len(steps[0].ToolResults) != 1 {
					t.Fatalf("%s tool results = %d, want 1", mode, len(steps[0].ToolResults))
				}
				got := steps[0].ToolResults[0].Result
				if fmt.Sprint(got) != fmt.Sprint(c.wantOut) {
					t.Fatalf("%s tool result = %v, want %v", mode, got, c.wantOut)
				}
			})
		})
	}
}

// TS 2473a71288 ('reports the last yielded value to onToolExecutionEnd' and
// 'writes one tool result chunk with the last yielded value to the writable').
func TestPortcarrywfGeneratorToolEndEventAndFinalChunk(t *testing.T) {
	var mu sync.Mutex
	var ends []ai.OnToolCallFinishEvent
	var chunks []provider.StreamChunk
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: portcarrywfWeatherSteps(),
		Tools: []types.Tool{portcarrywfGeneratorTool(func(yield func(interface{}, error) bool) {
			_ = yield(map[string]interface{}{"status": "loading"}, nil) && yield(map[string]interface{}{"temperature": 18}, nil)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Prompt:             "weather?",
		OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
		OnChunk:            func(c provider.StreamChunk) { mu.Lock(); chunks = append(chunks, c); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 || ends[0].ToolOutput.Error != nil || fmt.Sprint(ends[0].ToolOutput.Result) != "map[temperature:18]" {
		t.Fatalf("tool end events = %+v, want one success with the last yielded value", ends)
	}
	final := 0
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && !c.ToolResult.Preliminary {
			final++
			if fmt.Sprint(c.ToolResult.Result) != "map[temperature:18]" {
				t.Fatalf("final tool result chunk = %v, want the last yielded value", c.ToolResult.Result)
			}
		}
	}
	if final != 1 {
		t.Fatalf("final tool-result chunks = %d, want 1", final)
	}
}

func portcarrywfRefineTo(city string) map[string]ai.ToolInputRefiner {
	return map[string]ai.ToolInputRefiner{"weather": func(_ context.Context, o ai.ToolInputRefinementOptions) (map[string]interface{}, error) {
		out := map[string]interface{}{}
		for k, v := range o.ToolCall.Arguments {
			out[k] = v
		}
		out["city"] = city
		return out, nil
	}}
}

func portcarrywfAssistantCallInputs(req *provider.GenerateOptions) []string {
	var out []string
	for _, m := range req.Prompt.Messages {
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, p := range m.Content {
			if c, ok := p.(types.ToolCallContent); ok {
				out = append(out, portcarrywfCity(c.Arguments))
			}
		}
	}
	return out
}

// TS 9307549d51 (workflow-agent-refine-tool-input.test.ts 'refines the tool
// input in generate()/stream() like ToolLoopAgent', 'lets a per-call
// refinement override the constructor refinement', 'refines the input of a
// tool without a runtime validator'): the refined input reaches the tool, the
// start event, the step and the next request.
func TestPortcarrywfRefineToolInputInBothModes(t *testing.T) {
	{
		t.Run("constructor", func(t *testing.T) {
			var model *testutil.MockLanguageModel
			var mu sync.Mutex
			var executed, started []string
			portcarrywfRunBoth(t, func() WorkflowAgent {
				model = portcarrywfWeatherSteps()
				tool := portcarrywfWeatherTool(nil)
				tool.Execute = func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					mu.Lock()
					executed = append(executed, portcarrywfCity(in))
					mu.Unlock()
					return map[string]interface{}{"temperature": 18}, nil
				}
				cfg := WorkflowAgent{Model: model, Tools: []types.Tool{tool}, ExperimentalRefineToolInput: portcarrywfRefineTo("Paris"),
					OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
						mu.Lock()
						started = append(started, portcarrywfCity(e.ToolCall.Arguments))
						mu.Unlock()
					}}
				return cfg
			}, func(t *testing.T, mode string, steps []types.StepResult) {
				mu.Lock()
				defer mu.Unlock()
				if len(executed) != 1 || executed[0] != "Paris" || len(started) != 1 || started[0] != "Paris" {
					t.Fatalf("%s executed %v started %v, want Paris once", mode, executed, started)
				}
				if got := portcarrywfCity(steps[0].ToolCalls[0].Arguments); got != "Paris" {
					t.Fatalf("%s step tool call input = %q, want Paris", mode, got)
				}
				if got := portcarrywfCity(steps[0].ToolResults[0].Input); got != "Paris" {
					t.Fatalf("%s step tool result input = %q, want Paris", mode, got)
				}
				calls := model.GenerateCalls
				if mode == "stream" {
					calls = model.StreamCalls
				}
				if got := portcarrywfAssistantCallInputs(calls[1]); len(got) != 1 || got[0] != "Paris" {
					t.Fatalf("%s next request tool call inputs = %v, want [Paris]", mode, got)
				}
				executed, started = nil, nil
			})
		})
	}
}

// TS 9307549d51 ('lets a per-call refinement override the constructor
// refinement').
func TestPortcarrywfPerCallRefineToolInputOverridesConstructor(t *testing.T) {
	var executed []string
	tool := portcarrywfWeatherTool(nil)
	tool.Execute = func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		executed = append(executed, portcarrywfCity(in))
		return "ok", nil
	}
	for _, stream := range []bool{false, true} {
		executed = nil
		w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfWeatherSteps(), Tools: []types.Tool{tool}, ExperimentalRefineToolInput: portcarrywfRefineTo("Berlin")})
		if err != nil {
			t.Fatal(err)
		}
		if stream {
			res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", ExperimentalRefineToolInput: portcarrywfRefineTo("Paris")})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = res.ReadAll()
		} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", ExperimentalRefineToolInput: portcarrywfRefineTo("Paris")}); err != nil {
			t.Fatal(err)
		}
		if len(executed) != 1 || executed[0] != "Paris" {
			t.Fatalf("stream=%v executed %v, want [Paris]", stream, executed)
		}
	}
}

// TS 9307549d51 ('keeps tools without a refinement entry unchanged').
func TestPortcarrywfRefineToolInputLeavesOtherToolsAlone(t *testing.T) {
	var executed []string
	clock := types.Tool{Name: "clock", Description: "c", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"timezone": map[string]interface{}{"type": "string"}}},
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			executed = append(executed, fmt.Sprint(in["timezone"]))
			return "12:00", nil
		}}
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: portcarrywfModel(portcarrywfToolStep(portcarrywfCall("clock-call", "clock", map[string]interface{}{"timezone": "UTC"})), portcarrywfTextStep("It is 12:00.")),
		Tools: []types.Tool{portcarrywfWeatherTool(nil), clock}, ExperimentalRefineToolInput: portcarrywfRefineTo("Paris"),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(executed) != 1 || executed[0] != "UTC" || res.Steps[0].ToolCalls[0].Arguments["timezone"] != "UTC" {
		t.Fatalf("executed %v, step input %v, want UTC unchanged", executed, res.Steps[0].ToolCalls[0].Arguments)
	}
}

// TS 9307549d51 ('does not run a tool without a runtime validator when its
// refinement throws'): the call turns invalid and never runs.
func TestPortcarrywfRefineToolInputThrowingMakesTheCallInvalid(t *testing.T) {
	portcarrywfRunBoth(t, func() WorkflowAgent {
		return WorkflowAgent{
			Model: portcarrywfWeatherSteps(),
			Tools: []types.Tool{func() types.Tool {
				tool := portcarrywfWeatherTool(nil)
				tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					t.Error("tool ran although its input refinement failed")
					return nil, nil
				}
				return tool
			}()},
			ExperimentalRefineToolInput: map[string]ai.ToolInputRefiner{"weather": func(context.Context, ai.ToolInputRefinementOptions) (map[string]interface{}, error) {
				return nil, errors.New("refinement failed")
			}},
		}
	}, func(t *testing.T, mode string, steps []types.StepResult) {
		if len(steps[0].ToolCalls) != 1 || !steps[0].ToolCalls[0].Invalid {
			t.Fatalf("%s step tool calls = %+v, want one invalid call", mode, steps[0].ToolCalls)
		}
	})
}

// TS 9307549d51 ('runs an approved tool call with the refined input when
// generate()/stream() resumes' and the non-idempotent refinement variant): the
// input the user approved is the refined input, and the resume refines the
// original input again instead of the refined one.
func TestPortcarrywfApprovedCallRunsWithTheRefinedInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		refine map[string]ai.ToolInputRefiner
		want   string
	}{
		{"idempotent", portcarrywfRefineTo("Paris"), "Paris"},
		{"not idempotent", map[string]ai.ToolInputRefiner{"weather": func(_ context.Context, o ai.ToolInputRefinementOptions) (map[string]interface{}, error) {
			return map[string]interface{}{"city": portcarrywfCity(o.ToolCall.Arguments) + "!"}, nil
		}}, "London!"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s stream=%v", tc.name, stream), func(t *testing.T) {
				var executed []string
				tool := portcarrywfWeatherTool(nil)
				tool.ToolApproval = true
				tool.Execute = func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					executed = append(executed, portcarrywfCity(in))
					return "ok", nil
				}
				w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfWeatherSteps(), Tools: []types.Tool{tool}, ExperimentalRefineToolInput: tc.refine})
				if err != nil {
					t.Fatal(err)
				}
				user := types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "weather?"}}}
				run := func(messages []types.Message) []types.Message {
					if !stream {
						res, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: messages})
						if err != nil {
							t.Fatal(err)
						}
						return res.ResponseMessages
					}
					res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: messages})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := res.ReadAll(); err != nil {
						t.Fatal(err)
					}
					var out []types.Message
					for _, s := range res.Steps() {
						out = append(out, s.Response.Messages...)
					}
					return out
				}
				issued := run([]types.Message{user})
				if len(executed) != 0 {
					t.Fatalf("tool ran before approval: %v", executed)
				}
				resume := append([]types.Message{user}, issued...)
				resume = append(resume, types.Message{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolApprovalResponseContent{ApprovalID: "approval-weather-call", Approved: true},
				}})
				run(resume)
				if len(executed) != 1 || executed[0] != tc.want {
					t.Fatalf("executed %v, want [%s]", executed, tc.want)
				}
			})
		}
	}
}

// portcarrywfCaptureWarnings installs a custom warning logger for the test.
func portcarrywfCaptureWarnings(t *testing.T) func() []ai.LogWarningsOptions {
	t.Helper()
	var mu sync.Mutex
	var got []ai.LogWarningsOptions
	ai.SetLogWarnings(func(o ai.LogWarningsOptions) { mu.Lock(); got = append(got, o); mu.Unlock() })
	t.Cleanup(func() { ai.SetLogWarnings(nil) })
	return func() []ai.LogWarningsOptions {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
}

func portcarrywfWarnStep(step []provider.StreamChunk) []provider.StreamChunk {
	return append([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{{Type: "unsupported", Feature: "temperature"}}}}, step...)
}

// TS 7ce7ffc28b (workflow-agent-warnings.test.ts): the warnings of every model
// call are logged once, with the provider and model, in generate and stream; no
// warnings log nothing; a disabled logger stays silent.
func TestPortcarrywfLogsProviderWarningsOfEveryModelCall(t *testing.T) {
	cases := []struct {
		name  string
		steps [][]provider.StreamChunk
		want  int
	}{
		{"one call", [][]provider.StreamChunk{portcarrywfWarnStep(portcarrywfTextStep("Done."))}, 1},
		{"two calls", [][]provider.StreamChunk{
			portcarrywfWarnStep(portcarrywfToolStep(portcarrywfCall("weather-call", "weather", map[string]interface{}{"city": "London"}))),
			portcarrywfWarnStep(portcarrywfTextStep("It is 18C.")),
		}, 2},
		{"no warnings", [][]provider.StreamChunk{portcarrywfTextStep("Done.")}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logged := portcarrywfCaptureWarnings(t)
			portcarrywfRunBoth(t, func() WorkflowAgent {
				return WorkflowAgent{Model: portcarrywfModel(c.steps...), Tools: []types.Tool{portcarrywfWeatherTool(nil)}}
			}, func(t *testing.T, mode string, _ []types.StepResult) {
				got := logged()
				if len(got) != c.want {
					t.Fatalf("%s logged %d batches, want %d", mode, len(got), c.want)
				}
				for _, o := range got {
					if o.Provider != "mock-provider" || o.Model != "mock-model-id" || len(o.Warnings) != 1 || o.Warnings[0].Feature != "temperature" {
						t.Fatalf("%s logged %+v, want the temperature warning for mock-provider / mock-model-id", mode, o)
					}
				}
			})
		})
	}
}

func portcarrywfDefaultLimitWarnings(got []ai.LogWarningsOptions) []string {
	var out []string
	for _, o := range got {
		for _, w := range o.Warnings {
			if strings.Contains(w.Message, "default stopWhen condition") {
				out = append(out, w.Message)
			}
		}
	}
	return out
}

func portcarrywfAlwaysToolCalls(n int) [][]provider.StreamChunk {
	steps := make([][]provider.StreamChunk, n)
	for i := range steps {
		steps[i] = portcarrywfToolStep(portcarrywfCall(fmt.Sprintf("weather-%d", i), "weather", map[string]interface{}{"city": "London"}))
	}
	return steps
}

// TS f43ee39373 (workflow-agent-default-step-limit.test.ts): generate() stops
// at the default 20-step limit with one warning; an explicit limit (agent,
// call, prepareCall) or a natural finish on step 20 logs none; prepareCall
// returning the default condition it received still warns; stream() has no
// default limit.
func TestPortcarrywfDefaultStepLimitWarning(t *testing.T) {
	weather := func() []types.Tool { return []types.Tool{portcarrywfWeatherTool(nil)} }
	providerExecuted := portcarrywfToolStep(
		provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "provider-call", ToolName: "weather", Arguments: map[string]interface{}{"city": "London"}, ProviderExecuted: true}},
		provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "provider-call", ToolName: "weather", Result: map[string]interface{}{"temperature": 18}, ProviderExecuted: true}},
	)
	cases := []struct {
		name      string
		steps     [][]provider.StreamChunk
		agent     func(*WorkflowAgent)
		call      func(*WorkflowGenerateOptions)
		wantSteps int
		wantWarn  int
	}{
		{"default limit", portcarrywfAlwaysToolCalls(25), nil, nil, 20, 1},
		{"natural finish on step 20", append(portcarrywfAlwaysToolCalls(19), portcarrywfTextStep("Done.")), nil, nil, 20, 0},
		{"provider-executed call on step 20", append(portcarrywfAlwaysToolCalls(19), providerExecuted), nil, nil, 20, 0},
		{"explicit default limit on the agent", portcarrywfAlwaysToolCalls(25), func(w *WorkflowAgent) { w.StopWhen = []ai.StopCondition{ai.IsStepCount(20)} }, nil, 20, 0},
		{"explicit default limit on the call", portcarrywfAlwaysToolCalls(25), nil, func(o *WorkflowGenerateOptions) { o.StopWhen = []ai.StopCondition{ai.IsStepCount(20)} }, 20, 0},
		{"prepareCall provides a stop condition", portcarrywfAlwaysToolCalls(25), func(w *WorkflowAgent) {
			w.PrepareCall = func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				o.StopWhen = []ai.StopCondition{ai.IsStepCount(20)}
				return o, nil
			}
		}, nil, 20, 0},
		{"prepareCall returns the default condition", portcarrywfAlwaysToolCalls(25), func(w *WorkflowAgent) {
			w.PrepareCall = func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				return LanguageModelCallOptions{StopWhen: o.StopWhen}, nil
			}
		}, nil, 20, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logged := portcarrywfCaptureWarnings(t)
			cfg := WorkflowAgent{Model: portcarrywfModel(c.steps...), Tools: weather()}
			if c.agent != nil {
				c.agent(&cfg)
			}
			w, err := NewWorkflowAgent(cfg)
			if err != nil {
				t.Fatal(err)
			}
			call := WorkflowGenerateOptions{Prompt: "Keep calling the weather tool."}
			if c.call != nil {
				c.call(&call)
			}
			res, err := w.GenerateWithOptions(context.Background(), call)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Steps) != c.wantSteps {
				t.Fatalf("steps = %d, want %d", len(res.Steps), c.wantSteps)
			}
			warns := portcarrywfDefaultLimitWarnings(logged())
			if len(warns) != c.wantWarn {
				t.Fatalf("default-limit warnings = %v, want %d", warns, c.wantWarn)
			}
			if c.wantWarn == 1 && !strings.Contains(warns[0], "The tool loop stopped because it reached the default stopWhen condition, IsStepCount(20).") {
				t.Fatalf("warning = %q", warns[0])
			}
		})
	}
	t.Run("stream with an explicit limit", func(t *testing.T) {
		logged := portcarrywfCaptureWarnings(t)
		w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfModel(portcarrywfAlwaysToolCalls(25)...), Tools: weather()})
		if err != nil {
			t.Fatal(err)
		}
		res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go", StopWhen: []ai.StopCondition{ai.IsStepCount(25)}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = res.ReadAll()
		if len(res.Steps()) != 25 || len(portcarrywfDefaultLimitWarnings(logged())) != 0 {
			t.Fatalf("steps = %d, want 25 and no default-limit warning", len(res.Steps()))
		}
	})
}
