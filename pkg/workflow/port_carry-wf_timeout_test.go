package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// portcarrywfSegment is a part of a scripted provider stream: the chunks are
// delivered after the delay, and the stream ends after a closing segment.
type portcarrywfSegment struct {
	after  time.Duration
	chunks []provider.StreamChunk
	close  bool
}

type portcarrywfScheduledStream struct {
	ctx        context.Context
	honorAbort bool
	out        chan provider.StreamChunk
	done       chan struct{}
	once       sync.Once
}

func (s *portcarrywfScheduledStream) Next() (*provider.StreamChunk, error) {
	var abort <-chan struct{}
	if s.honorAbort {
		abort = s.ctx.Done()
	}
	select {
	case c, ok := <-s.out:
		if !ok {
			return nil, io.EOF
		}
		return &c, nil
	case <-abort:
		return nil, context.Cause(s.ctx)
	case <-s.done:
		return nil, errors.New("stream closed")
	}
}
func (s *portcarrywfScheduledStream) Err() error { return nil }
func (s *portcarrywfScheduledStream) Close() error {
	s.once.Do(func() { close(s.done) })
	return nil
}

type portcarrywfScheduledModel struct {
	*testutil.MockLanguageModel
	mu      sync.Mutex
	signals []context.Context
}

func (m *portcarrywfScheduledModel) contexts() []context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]context.Context(nil), m.signals...)
}

// portcarrywfScheduledStreamModel plays the segments returned for each call. By
// default the stream stays open after the last segment, the state a timeout
// has to end; honorAbort false makes the provider ignore the call's context.
func portcarrywfScheduledStreamModel(segmentsForCall func(call int) []portcarrywfSegment, honorAbort bool) *portcarrywfScheduledModel {
	m := &portcarrywfScheduledModel{}
	m.MockLanguageModel = &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id", ToolSupport: true,
		DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			m.mu.Lock()
			call := len(m.signals)
			m.signals = append(m.signals, ctx)
			m.mu.Unlock()
			s := &portcarrywfScheduledStream{ctx: ctx, honorAbort: honorAbort, out: make(chan provider.StreamChunk, 256), done: make(chan struct{})}
			go func() {
				for _, seg := range segmentsForCall(call) {
					if seg.after > 0 {
						select {
						case <-time.After(seg.after):
						case <-s.done:
							return
						}
					}
					for _, c := range seg.chunks {
						select {
						case s.out <- c:
						case <-s.done:
							return
						}
					}
					if seg.close {
						close(s.out)
						return
					}
				}
			}()
			return s, nil
		},
	}
	return m
}

func portcarrywfTextStart(id string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id}
}

func portcarrywfTextDelta(id, delta string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: delta}
}

func portcarrywfFinishStop() provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: portcarrywfInt(3), OutputTokens: portcarrywfInt(2)}}
}

func portcarrywfDur(ms int) *time.Duration {
	d := time.Duration(ms) * time.Millisecond
	return &d
}

type portcarrywfTimeoutRun struct {
	steps   []types.StepResult
	aborts  int
	errs    int
	ends    int
	elapsed time.Duration
}

func portcarrywfStreamWithTimeout(t *testing.T, model provider.LanguageModel, timeout *ai.TimeoutConfig, maxRetries int, tools []types.Tool) portcarrywfTimeoutRun {
	t.Helper()
	var mu sync.Mutex
	var run portcarrywfTimeoutRun
	w, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: tools, MaxRetries: &maxRetries})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Prompt:  "What is the weather in London?",
		Timeout: timeout,
		OnAbort: func(context.Context, []types.StepResult) { mu.Lock(); run.aborts++; mu.Unlock() },
		OnError: func(context.Context, error) { mu.Lock(); run.errs++; mu.Unlock() },
		OnEnd:   func(context.Context, ai.OnFinishEvent) { mu.Lock(); run.ends++; mu.Unlock() },
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	_, _ = res.ReadAll()
	run.elapsed = time.Since(start)
	mu.Lock()
	defer mu.Unlock()
	run.steps = res.Steps()
	return run
}

func portcarrywfWantCause(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	cause := context.Cause(ctx)
	var te *ai.TimeoutError
	if !errors.As(cause, &te) || cause.Error() != want {
		t.Fatalf("model call cause = %v, want TimeoutError %q", cause, want)
	}
}

func portcarrywfHelloSegments() []portcarrywfSegment {
	return []portcarrywfSegment{{chunks: []provider.StreamChunk{portcarrywfTextStart("text-1"), portcarrywfTextDelta("text-1", "Hello")}}}
}

// TS f1de9cb61f (workflow-agent-timeout-contract.test.ts 'stream() with chunkMs
// / firstChunkMs / stepMs / totalMs'): each timeout ends the stream as an
// abort with its own reason; no steps, no onError, no onEnd.
func TestPortcarrywfStreamTimeoutsEndTheRunAsAnAbort(t *testing.T) {
	cases := []struct {
		name     string
		timeout  *ai.TimeoutConfig
		segments []portcarrywfSegment
		cause    string
	}{
		{"chunk", &ai.TimeoutConfig{PerChunk: portcarrywfDur(100)}, portcarrywfHelloSegments(), "Chunk timeout of 100ms exceeded"},
		{"first chunk, only non-output parts", &ai.TimeoutConfig{FirstChunk: portcarrywfDur(100)}, []portcarrywfSegment{{chunks: []provider.StreamChunk{portcarrywfTextStart("text-1"), portcarrywfTextDelta("text-1", "")}}}, "First chunk timeout of 100ms exceeded"},
		{"step", &ai.TimeoutConfig{PerStep: portcarrywfDur(100)}, portcarrywfHelloSegments(), "Step timeout of 100ms exceeded"},
		{"total", &ai.TimeoutConfig{Total: portcarrywfDur(100)}, portcarrywfHelloSegments(), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model := portcarrywfScheduledStreamModel(func(int) []portcarrywfSegment { return c.segments }, true)
			run := portcarrywfStreamWithTimeout(t, model, c.timeout, 0, nil)
			if run.elapsed > 1500*time.Millisecond {
				t.Fatalf("took %v, want the timeout to end the run", run.elapsed)
			}
			if len(run.steps) != 0 || run.aborts != 1 || run.errs != 0 || run.ends != 0 {
				t.Fatalf("steps/aborts/errors/ends = %d/%d/%d/%d, want 0/1/0/0", len(run.steps), run.aborts, run.errs, run.ends)
			}
			ctxs := model.contexts()
			if len(ctxs) != 1 {
				t.Fatalf("model calls = %d, want 1", len(ctxs))
			}
			if c.cause != "" {
				portcarrywfWantCause(t, ctxs[0], c.cause)
			}
		})
	}
}

// TS f1de9cb61f ('does not reset the chunk timer for empty text deltas' and
// the text, reasoning and tool input delta variants that do reset it).
func TestPortcarrywfChunkTimerResetsOnlyOnOutput(t *testing.T) {
	t.Run("empty text deltas do not reset it", func(t *testing.T) {
		segments := []portcarrywfSegment{{chunks: []provider.StreamChunk{portcarrywfTextStart("text-1"), portcarrywfTextDelta("text-1", "Hello")}}}
		for i := 0; i < 20; i++ {
			segments = append(segments, portcarrywfSegment{after: 20 * time.Millisecond, chunks: []provider.StreamChunk{portcarrywfTextDelta("text-1", "")}})
		}
		model := portcarrywfScheduledStreamModel(func(int) []portcarrywfSegment { return segments }, true)
		run := portcarrywfStreamWithTimeout(t, model, &ai.TimeoutConfig{PerChunk: portcarrywfDur(100)}, 0, nil)
		if len(run.steps) != 0 || run.aborts != 1 {
			t.Fatalf("steps/aborts = %d/%d, want 0/1", len(run.steps), run.aborts)
		}
		portcarrywfWantCause(t, model.contexts()[0], "Chunk timeout of 100ms exceeded")
	})
	deltas := func(kind provider.ChunkType, ids ...string) []portcarrywfSegment {
		segs := []portcarrywfSegment{}
		for _, d := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
			segs = append(segs, portcarrywfSegment{after: 40 * time.Millisecond, chunks: []provider.StreamChunk{{Type: kind, ID: ids[0], Text: d, Reasoning: d}}})
		}
		return segs
	}
	t.Run("text deltas reset it", func(t *testing.T) {
		segments := append([]portcarrywfSegment{{chunks: []provider.StreamChunk{portcarrywfTextStart("text-1")}}}, deltas(provider.ChunkTypeText, "text-1")...)
		segments = append(segments, portcarrywfSegment{chunks: []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "text-1"}, portcarrywfFinishStop()}, close: true})
		model := portcarrywfScheduledStreamModel(func(int) []portcarrywfSegment { return segments }, true)
		run := portcarrywfStreamWithTimeout(t, model, &ai.TimeoutConfig{PerChunk: portcarrywfDur(150)}, 0, nil)
		if len(run.steps) != 1 || run.steps[0].Text != "abcdefgh" {
			t.Fatalf("steps = %d, text %q, want one step abcdefgh", len(run.steps), func() string {
				if len(run.steps) > 0 {
					return run.steps[0].Text
				}
				return ""
			}())
		}
	})
	t.Run("reasoning deltas reset it", func(t *testing.T) {
		segments := append([]portcarrywfSegment{{chunks: []provider.StreamChunk{{Type: provider.ChunkTypeReasoningStart, ID: "r-1"}}}}, deltas(provider.ChunkTypeReasoning, "r-1")...)
		segments = append(segments, portcarrywfSegment{chunks: []provider.StreamChunk{{Type: provider.ChunkTypeReasoningEnd, ID: "r-1"}, portcarrywfFinishStop()}, close: true})
		model := portcarrywfScheduledStreamModel(func(int) []portcarrywfSegment { return segments }, true)
		run := portcarrywfStreamWithTimeout(t, model, &ai.TimeoutConfig{PerChunk: portcarrywfDur(150)}, 0, nil)
		if len(run.steps) != 1 || run.steps[0].ReasoningText != "abcdefgh" {
			t.Fatalf("steps = %d, want one step with reasoning abcdefgh (aborts %d)", len(run.steps), run.aborts)
		}
	})
	t.Run("tool input deltas reset it", func(t *testing.T) {
		segments := []portcarrywfSegment{{chunks: []provider.StreamChunk{{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather"}}}}}
		for _, d := range []string{`{"ci`, `ty":`, `"Lon`, `don"}`} {
			segments = append(segments, portcarrywfSegment{after: 40 * time.Millisecond, chunks: []provider.StreamChunk{{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: d}}})
		}
		call := portcarrywfCall("call-1", "weather", map[string]interface{}{"city": "London"})
		segments = append(segments, portcarrywfSegment{chunks: []provider.StreamChunk{
			{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"}, call,
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{InputTokens: portcarrywfInt(3), OutputTokens: portcarrywfInt(2)}},
		}, close: true})
		model := portcarrywfScheduledStreamModel(func(int) []portcarrywfSegment { return segments }, true)
		// A tool without Execute ends the run after the first step.
		noExec := types.Tool{Name: "weather", Description: "w", Parameters: portcarrywfCitySchema()}
		run := portcarrywfStreamWithTimeout(t, model, &ai.TimeoutConfig{PerChunk: portcarrywfDur(150)}, 0, []types.Tool{noExec})
		if len(run.steps) != 1 || len(run.steps[0].ToolCalls) != 1 || run.steps[0].ToolCalls[0].ToolName != "weather" {
			t.Fatalf("steps = %d (aborts %d), want one step with the weather call", len(run.steps), run.aborts)
		}
	})
}

// TS f1de9cb61f ('applies the first-chunk timeout to every model call').
func TestPortcarrywfFirstChunkTimeoutAppliesToEveryModelCall(t *testing.T) {
	model := portcarrywfScheduledStreamModel(func(call int) []portcarrywfSegment {
		if call == 0 {
			return []portcarrywfSegment{{chunks: portcarrywfToolStep(portcarrywfCall("weather-call", "weather", map[string]interface{}{"city": "London"})), close: true}}
		}
		return []portcarrywfSegment{{chunks: []provider.StreamChunk{portcarrywfTextStart("text-1")}}}
	}, true)
	run := portcarrywfStreamWithTimeout(t, model, &ai.TimeoutConfig{FirstChunk: portcarrywfDur(100)}, 0, []types.Tool{portcarrywfWeatherTool(nil)})
	ctxs := model.contexts()
	if len(run.steps) != 1 || run.aborts != 1 || len(ctxs) != 2 {
		t.Fatalf("steps/aborts/model calls = %d/%d/%d, want 1/1/2", len(run.steps), run.aborts, len(ctxs))
	}
	portcarrywfWantCause(t, ctxs[1], "First chunk timeout of 100ms exceeded")
}

// TS f1de9cb61f ('ends when the provider ignores the abort signal'): the run
// ends at the timeout although the provider stream never reports the abort.
func TestPortcarrywfStreamEndsWhenTheProviderIgnoresTheAbort(t *testing.T) {
	for _, timeout := range []*ai.TimeoutConfig{{PerChunk: portcarrywfDur(100)}, {Total: portcarrywfDur(100)}} {
		model := portcarrywfScheduledStreamModel(func(int) []portcarrywfSegment { return portcarrywfHelloSegments() }, false)
		run := portcarrywfStreamWithTimeout(t, model, timeout, 0, nil)
		if run.elapsed > 1500*time.Millisecond || len(run.steps) != 0 || run.aborts != 1 {
			t.Fatalf("elapsed %v steps %d aborts %d, want a prompt abort with no steps", run.elapsed, len(run.steps), run.aborts)
		}
	}
}

// TS f1de9cb61f ('ends stream() quickly when an external signal aborts before
// a long step timeout').
func TestPortcarrywfExternalAbortBeatsALongStepTimeout(t *testing.T) {
	model := portcarrywfScheduledStreamModel(func(int) []portcarrywfSegment {
		return []portcarrywfSegment{{chunks: []provider.StreamChunk{portcarrywfTextStart("text-1")}}}
	}, true)
	zero := 0
	w, err := NewWorkflowAgent(WorkflowAgent{Model: model, MaxRetries: &zero})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)
	aborts := 0
	start := time.Now()
	res, err := w.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi", Timeout: &ai.TimeoutConfig{PerStep: portcarrywfDur(5000)}, OnAbort: func(context.Context, []types.StepResult) { aborts++ }})
	if err == nil {
		_, _ = res.ReadAll()
	}
	if time.Since(start) > 1500*time.Millisecond || aborts != 1 {
		t.Fatalf("elapsed %v aborts %d, want a prompt abort", time.Since(start), aborts)
	}
}

func portcarrywfHangingGenerateModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}}
}

// TS f1de9cb61f ('rejects generate() with a step timeout' and 'with a total
// timeout').
func TestPortcarrywfGenerateRejectsWithTheTimeoutError(t *testing.T) {
	zero := 0
	for name, c := range map[string]struct {
		timeout *ai.TimeoutConfig
		want    string
	}{
		"step":  {&ai.TimeoutConfig{PerStep: portcarrywfDur(100)}, "Step timeout of 100ms exceeded"},
		"total": {&ai.TimeoutConfig{Total: portcarrywfDur(100)}, ""},
	} {
		w, err := NewWorkflowAgent(WorkflowAgent{Model: portcarrywfHangingGenerateModel(), MaxRetries: &zero})
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", Timeout: c.timeout})
		var te *ai.TimeoutError
		if !errors.As(err, &te) || (c.want != "" && err.Error() != c.want) {
			t.Fatalf("%s: err = %v, want a TimeoutError %q", name, err, c.want)
		}
	}
}

// TS f1de9cb61f ('ignores firstChunkMs and chunkMs in generate()').
func TestPortcarrywfGenerateIgnoresChunkTimeouts(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		time.Sleep(60 * time.Millisecond)
		return &types.GenerateResult{Text: "It is 18C.", FinishReason: types.FinishReasonStop}, nil
	}}
	zero := 0
	w, err := NewWorkflowAgent(WorkflowAgent{Model: model, MaxRetries: &zero})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", Timeout: &ai.TimeoutConfig{FirstChunk: portcarrywfDur(10), PerChunk: portcarrywfDur(10)}})
	if err != nil || len(res.Steps) != 1 || res.Text != "It is 18C." {
		t.Fatalf("res %+v err %v, want one step with the text", res, err)
	}
}

// TS f1de9cb61f ('starts the step timer again for every model call').
func TestPortcarrywfStepTimerRestartsForEveryModelCall(t *testing.T) {
	slow := func() *testutil.MockLanguageModel {
		m := portcarrywfWeatherSteps()
		stream, generate := m.DoStreamFunc, m.DoGenerateFunc
		m.DoStreamFunc = func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			time.Sleep(80 * time.Millisecond)
			return stream(ctx, o)
		}
		m.DoGenerateFunc = func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			time.Sleep(80 * time.Millisecond)
			return generate(ctx, o)
		}
		return m
	}
	portcarrywfRunBoth(t, func() WorkflowAgent {
		zero := 0
		return WorkflowAgent{Model: slow(), Tools: []types.Tool{portcarrywfWeatherTool(nil)}, MaxRetries: &zero, Timeout: &ai.TimeoutConfig{PerStep: portcarrywfDur(150)}}
	}, func(t *testing.T, mode string, steps []types.StepResult) {
		if len(steps) != 2 || steps[1].Text != "It is 18C." {
			t.Fatalf("%s steps = %d, want 2 with the final text", mode, len(steps))
		}
	})
}

// TS f1de9cb61f ('counts SDK retries inside the step timeout').
func TestPortcarrywfRetriesCountInsideTheStepTimeout(t *testing.T) {
	var mu sync.Mutex
	var ctxs []context.Context
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		ctxs = append(ctxs, ctx)
		n := len(ctxs)
		mu.Unlock()
		if n == 1 {
			failure := providererrors.NewProviderError("mock-provider", 503, "", "Retryable fixture failure", nil)
			failure.ResponseHeaders = map[string]string{"retry-after-ms": "0"}
			return nil, failure
		}
		s := &portcarrywfScheduledStream{ctx: ctx, honorAbort: true, out: make(chan provider.StreamChunk, 4), done: make(chan struct{})}
		s.out <- portcarrywfTextStart("text-1")
		return s, nil
	}}
	run := portcarrywfStreamWithTimeout(t, model, &ai.TimeoutConfig{PerStep: portcarrywfDur(200)}, 2, nil)
	mu.Lock()
	defer mu.Unlock()
	if len(ctxs) != 2 || len(run.steps) != 0 || run.aborts != 1 {
		t.Fatalf("model calls/steps/aborts = %d/%d/%d, want 2/0/1", len(ctxs), len(run.steps), run.aborts)
	}
	portcarrywfWantCause(t, ctxs[1], "Step timeout of 200ms exceeded")
}

// TS f1de9cb61f ('treats a negative number as a deadline that has passed'):
// the call ends as timed out before its first model call.
func TestPortcarrywfNegativeTotalIsADeadlineThatHasPassed(t *testing.T) {
	negative := -time.Millisecond
	model := portcarrywfScheduledStreamModel(func(int) []portcarrywfSegment {
		return []portcarrywfSegment{{chunks: []provider.StreamChunk{portcarrywfTextStart("t"), portcarrywfFinishStop()}, close: true}}
	}, true)
	zero := 0
	aborts := 0
	sw, err := NewWorkflowAgent(WorkflowAgent{Model: model, MaxRetries: &zero})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sw.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Prompt: "hi", Timeout: &ai.TimeoutConfig{Total: &negative},
		OnAbort: func(context.Context, []types.StepResult) { aborts++ },
	})
	if err == nil {
		_, err = res.ReadAll()
	}
	var streamTimeout *ai.TimeoutError
	if !errors.As(err, &streamTimeout) || aborts != 1 || len(model.contexts()) != 0 {
		t.Fatalf("stream err = %v, aborts %d, model calls %d, want a TimeoutError, 1 abort and no model call", err, aborts, len(model.contexts()))
	}
	gen := &testutil.MockLanguageModel{}
	w, err := NewWorkflowAgent(WorkflowAgent{Model: gen, MaxRetries: &zero})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", Timeout: &ai.TimeoutConfig{Total: &negative}})
	var te *ai.TimeoutError
	if !errors.As(err, &te) || len(gen.GenerateCalls) != 0 {
		t.Fatalf("generate err = %v, model calls %d, want a TimeoutError before any model call", err, len(gen.GenerateCalls))
	}
}

// TS f1de9cb61f ('stream()/generate() rejects ... before the model call'): a
// timeout the timers cannot honor (negative per-step/first/chunk, above the
// 2^31-1 ms timer limit) is an InvalidArgumentError naming the field.
func TestPortcarrywfInvalidTimeoutValuesAreRejectedBeforeTheModelCall(t *testing.T) {
	tooLong := time.Duration(1<<31) * time.Millisecond
	negative := -time.Millisecond
	cases := []struct {
		name      string
		timeout   ai.TimeoutConfig
		parameter string
	}{
		{"a total above the timer limit", ai.TimeoutConfig{Total: &tooLong}, "timeout.totalMs"},
		{"a negative stepMs", ai.TimeoutConfig{PerStep: &negative}, "timeout.stepMs"},
		{"a negative firstChunkMs", ai.TimeoutConfig{FirstChunk: &negative}, "timeout.firstChunkMs"},
		{"a chunkMs above the timer limit", ai.TimeoutConfig{PerChunk: &tooLong}, "timeout.chunkMs"},
	}
	for _, c := range cases {
		for _, mode := range []string{"stream", "generate"} {
			t.Run(fmt.Sprintf("%s/%s", mode, c.name), func(t *testing.T) {
				model := &testutil.MockLanguageModel{}
				zero := 0
				w, err := NewWorkflowAgent(WorkflowAgent{Model: model, MaxRetries: &zero})
				if err != nil {
					t.Fatal(err)
				}
				timeout := c.timeout
				if mode == "stream" {
					_, err = w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", Timeout: &timeout})
				} else {
					_, err = w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", Timeout: &timeout})
				}
				var invalid *providererrors.InvalidArgumentError
				if !errors.As(err, &invalid) {
					t.Fatalf("err = %v (%T), want an InvalidArgumentError", err, err)
				}
				if !portcarrywfContainsAll(err.Error(), c.parameter) {
					t.Fatalf("err = %q, want it to name %s", err, c.parameter)
				}
				if len(model.GenerateCalls)+len(model.StreamCalls) != 0 {
					t.Fatal("the model was called")
				}
			})
		}
	}
}

func portcarrywfContainsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TS f1de9cb61f (workflow-agent-timeout.test.ts 'accepts the timer bounds',
// 'treats a negative total timeout as a deadline that has passed', 'returns no
// settings for an undefined timeout'): the bounds of the timer range are valid.
func TestPortcarrywfValidateTimeoutAcceptsTheTimerBounds(t *testing.T) {
	if err := validateWorkflowTimeout(nil); err != nil {
		t.Fatalf("nil timeout: %v", err)
	}
	max := time.Duration(workflowMaxTimeoutMs) * time.Millisecond
	zero := time.Duration(0)
	negative := -250 * time.Millisecond
	for name, cfg := range map[string]*ai.TimeoutConfig{
		"zero total":     {Total: &zero},
		"max everything": {Total: &max, PerStep: &max, FirstChunk: &max, PerChunk: &max},
		"zero others":    {PerStep: &zero, FirstChunk: &zero, PerChunk: &zero},
		"negative total": {Total: &negative},
	} {
		if err := validateWorkflowTimeout(cfg); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	over := max + time.Millisecond
	for name, cfg := range map[string]*ai.TimeoutConfig{
		"total over":     {Total: &over},
		"negative step":  {PerStep: &negative},
		"negative first": {FirstChunk: &negative},
		"negative chunk": {PerChunk: &negative},
		"chunk over":     {PerChunk: &over},
	} {
		if err := validateWorkflowTimeout(cfg); err == nil {
			t.Fatalf("%s: want an InvalidArgumentError", name)
		}
	}
}
