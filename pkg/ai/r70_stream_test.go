package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText pipes every run through createOutputTransformStream(output ??
// text(), retention.replay || output != null): without an output spec the
// partial output is the step's text so far (text().parsePartialOutput), and a
// low-memory run without a spec publishes none.
func TestR70StreamTextPartialOutputWithoutSpecIsText(t *testing.T) {
	chunks := func() []provider.StreamChunk {
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Hel"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "lo"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}
	}
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream(chunks()), nil
	}}

	t.Run("default retention", func(t *testing.T) {
		r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		if got := r.PartialOutput(); got != "Hello" {
			t.Fatalf("PartialOutput() = %#v, want %q", got, "Hello")
		}
	})

	t.Run("low memory", func(t *testing.T) {
		r, err := StreamTextLowMemory(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		if got := r.PartialOutput(); got != nil {
			t.Fatalf("low-memory PartialOutput() = %#v, want nil", got)
		}
	})
}

// TS streamText skips onChunk/onError downstream only for the error value the
// model stream reader already reported (errorsHandledForStreamRetry holds
// value.error): an error part a transform replaces with a different error
// reaches onChunk and onError again. Without an Err the part's error value is
// its text.
func TestR70StreamTextTransformedErrorPartReachesOnChunkAgain(t *testing.T) {
	errA, errB := errors.New("a"), errors.New("b")
	cases := []struct {
		name    string
		in, out provider.StreamChunk
		wantLog string
	}{
		{
			name:    "text only",
			in:      provider.StreamChunk{Type: provider.ChunkTypeError, Text: "a"},
			out:     provider.StreamChunk{Type: provider.ChunkTypeError, Text: "b"},
			wantLog: "chunk:error:a onError:a chunk:error:b onError:b",
		},
		{
			name:    "error values",
			in:      provider.StreamChunk{Type: provider.ChunkTypeError, Text: "a", Err: errA},
			out:     provider.StreamChunk{Type: provider.ChunkTypeError, Text: "b", Err: errB},
			wantLog: "chunk:error:a onError:a chunk:error:b onError:b",
		},
		{
			name:    "same text passes through",
			in:      provider.StreamChunk{Type: provider.ChunkTypeError, Text: "a"},
			out:     provider.StreamChunk{Type: provider.ChunkTypeError, Text: "a"},
			wantLog: "chunk:error:a onError:a",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var log r69EventLog
			opts := log.options(StreamTextOptions{
				Model: r69Model(r69Stream(tc.in, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError})),
				ExperimentalTransform: []StreamTransformFunc{func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
					if c.Type == provider.ChunkTypeError {
						return []provider.StreamChunk{tc.out}
					}
					return []provider.StreamChunk{c}
				}},
			})
			r, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			_ = r.ConsumeStream()
			got := strings.TrimPrefix(log.String(), "chunk:start ")
			if got != tc.wantLog {
				t.Fatalf("events = %q, want %q", got, tc.wantLog)
			}
		})
	}
}

// ---- TS stream-text.test.ts "errors" describe block ports (round 70) ----

func r70Parts(t *testing.T, r *StreamTextResult) []provider.StreamChunk {
	t.Helper()
	var parts []provider.StreamChunk
	s := r.FullStream()
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		parts = append(parts, *c)
	}
	return parts
}

func r70TextDeltas(parts []provider.StreamChunk) []string {
	var out []string
	for _, p := range parts {
		if p.Type == provider.ChunkTypeText {
			out = append(out, p.Text)
		}
	}
	return out
}

func r70Chunks(chunks ...provider.StreamChunk) func() (provider.TextStream, error) {
	return r69Stream(chunks...)
}

// r70Attempt is one model call with its own request body and response headers.
func r70Attempt(body string, chunks ...provider.StreamChunk) func() (provider.TextStream, error) {
	return func() (provider.TextStream, error) {
		return &requestBodyTextStream{TextStream: testutil.NewMockTextStream(chunks), body: body}, nil
	}
}

var (
	r70Stop     = provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop"}
	r70ProvErr  = errors.New("provider error")
	r70ErrPart  = provider.StreamChunk{Type: provider.ChunkTypeError, Err: r70ProvErr, Text: r70ProvErr.Error()}
	r70Schema   = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"x": map[string]interface{}{"type": "string"}}, "required": []interface{}{"x"}})
	r70ZeroInt  = 0
	r70OneRetry = 1
)

func r70Text(id, text string) []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: id},
		{Type: provider.ChunkTypeText, ID: id, Text: text},
	}
}

func r70Cat(groups ...[]provider.StreamChunk) []provider.StreamChunk {
	var out []provider.StreamChunk
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// TS 3076: should retry the current step after a provider error stream part.
func TestR70TSStreamRetryAfterProviderErrorPart(t *testing.T) {
	var calls int
	model := r69Model(
		func() (provider.TextStream, error) {
			calls++
			return testutil.NewMockTextStream(r70Cat(r70Text("1", "partial "), []provider.StreamChunk{r70ErrPart})), nil
		},
		func() (provider.TextStream, error) {
			calls++
			return testutil.NewMockTextStream(r70Cat(r70Text("2", "recovered"), []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "2"}, r70Stop})), nil
		},
	)
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", MaxRetries: &r70ZeroInt, StreamRetries: &r70OneRetry, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	parts := r70Parts(t, r)
	if got := r70TextDeltas(parts); strings.Join(got, "|") != "partial |recovered" {
		t.Fatalf("text deltas = %q", got)
	}
	if calls != 2 || r.FinishReason() != types.FinishReasonStop || r.Err() != nil {
		t.Fatalf("calls %d, finish %q, err %v", calls, r.FinishReason(), r.Err())
	}
}

// TS 3115: should exclude failed-attempt text from structured output state.
func TestR70TSStreamRetryExcludesFailedAttemptFromOutput(t *testing.T) {
	model := r69Model(
		r70Chunks(r70Cat(r70Text("1", `{"x":`), []provider.StreamChunk{r70ErrPart})...),
		r70Chunks(r70Cat(r70Text("2", `{"x":"ok"}`), []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "2"}, r70Stop})...),
	)
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
		Output:        ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r70Schema}),
		StreamRetries: &r70OneRetry, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if out, _ := r.Output().(map[string]interface{}); out["x"] != "ok" || len(out) != 1 {
		t.Fatalf("Output() = %#v (err %v)", r.Output(), r.OutputErr())
	}
	if got := r.FinalStep().Text; got != `{"x":"ok"}` {
		t.Fatalf("final step text = %q", got)
	}
}

// TS 3154 and 3358: the recovered attempt's request body, response metadata
// and content make up the step, also when a transform rebuilds the parts.
func TestR70TSStreamRetryUsesRecoveredAttemptMetadata(t *testing.T) {
	for _, withTransform := range []bool{false, true} {
		t.Run(fmt.Sprintf("transform=%v", withTransform), func(t *testing.T) {
			meta := func(id, model string, ts int64, call string) provider.StreamChunk {
				return provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{
					ID: id, ModelID: model, Timestamp: time.UnixMilli(ts), Headers: map[string]string{"call": call}}}
			}
			model := r69Model(
				r70Attempt("request-1", r70Cat([]provider.StreamChunk{meta("failed-id", "failed-model", 1, "1")}, r70Text("failed-text", `{"x":`), []provider.StreamChunk{r70ErrPart})...),
				r70Attempt("request-2", r70Cat([]provider.StreamChunk{meta("recovered-id", "recovered-model", 2, "2")}, r70Text("recovered-text", `{"x":"ok"}`),
					[]provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "recovered-text"}, r70Stop})...),
			)
			opts := StreamTextOptions{Model: model, Prompt: "test-input",
				Output:        ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r70Schema}),
				StreamRetries: &r70OneRetry, Include: &IncludeOptions{RequestBody: true}, OnError: func(context.Context, error) {}}
			if withTransform {
				opts.ExperimentalTransform = []StreamTransformFunc{func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
					if c.Type == provider.ChunkTypeTextStart {
						return []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: c.ID, ProviderMetadata: c.ProviderMetadata}}
					}
					return []provider.StreamChunk{c}
				}}
			}
			r, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			parts := r70Parts(t, r)
			if got := strings.Join(r70TextDeltas(parts), "|"); got != `{"x":|{"x":"ok"}` {
				t.Fatalf("text deltas = %q", got)
			}
			if out, _ := r.Output().(map[string]interface{}); out["x"] != "ok" {
				t.Fatalf("Output() = %#v (err %v), final step text %q", r.Output(), r.OutputErr(), r.FinalStep().Text)
			}
			fs := r.FinalStep()
			if fs.Text != `{"x":"ok"}` {
				t.Fatalf("final step text = %q", fs.Text)
			}
			if fs.Request.Body != "request-2" {
				t.Fatalf("request body = %#v", fs.Request.Body)
			}
			resp := fs.Response
			if resp.ID != "recovered-id" || resp.ModelID != "recovered-model" || !resp.Timestamp.Equal(time.UnixMilli(2)) || resp.Headers["call"] != "2" {
				t.Fatalf("response = %+v", resp)
			}
			if len(resp.Messages) != 1 || resp.Messages[0].Role != types.RoleAssistant {
				t.Fatalf("response messages = %+v", resp.Messages)
			}
			if txt := textOfContent(resp.Messages[0].Content); txt != `{"x":"ok"}` {
				t.Fatalf("response message text = %q", txt)
			}
			if !reflect.DeepEqual(r.ResponseMessages(), resp.Messages) {
				t.Fatalf("ResponseMessages() = %+v, want the final step's messages", r.ResponseMessages())
			}
		})
	}
}

// TS 3259: should reset failed-attempt state when the recovered stream ends
// without output.
func TestR70TSStreamRetryRecoveredStreamWithoutOutput(t *testing.T) {
	var calls int
	model := r69Model(
		func() (provider.TextStream, error) {
			calls++
			return testutil.NewMockTextStream(r70Cat(r70Text("failed-text", "partial"), []provider.StreamChunk{r70ErrPart})), nil
		},
		func() (provider.TextStream, error) { calls++; return testutil.NewMockTextStream(nil), nil },
	)
	var mu sync.Mutex
	var onErrors []error
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", StreamRetries: &r70OneRetry,
		OnError: func(_ context.Context, err error) { mu.Lock(); onErrors = append(onErrors, err); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	parts := r70Parts(t, r)
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
	var sawPartial bool
	var terminal *provider.StreamChunk
	for i := range parts {
		if parts[i].Type == provider.ChunkTypeText && parts[i].ID == "failed-text" && parts[i].Text == "partial" {
			sawPartial = true
		}
		if parts[i].Type == provider.ChunkTypeError && terminal == nil {
			terminal = &parts[i]
		}
	}
	if !sawPartial {
		t.Fatalf("no failed-attempt text delta in %v", parts)
	}
	if terminal == nil || !IsNoOutputGeneratedError(terminal.Err) {
		t.Fatalf("first error part = %+v, want NoOutputGeneratedError", terminal)
	}
	const msg = "No output generated. The model stream ended without a finish chunk."
	if r.Err() == nil || r.Err().Error() != msg {
		t.Fatalf("Err() = %v", r.Err())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(onErrors) != 2 || onErrors[0].Error() != "provider error" || onErrors[1].Error() != msg {
		t.Fatalf("onError calls = %v", onErrors)
	}
}

func r70Tool(name string, executed *atomic.Int32) types.Tool {
	return types.Tool{Name: name, Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			if executed != nil {
				executed.Add(1)
			}
			return "tool result", nil
		}}
}

func r70ToolCall(id string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}}
}

var r70ToolCallsFinish = provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool-calls"}

// TS 3419: should retry the current step when onError requests a retry.
func TestR70TSStreamCallbackRetry(t *testing.T) {
	var calls, onErrors, retryAsks atomic.Int32
	model := r69Model(
		func() (provider.TextStream, error) {
			calls.Add(1)
			return testutil.NewMockTextStream([]provider.StreamChunk{r70ErrPart}), nil
		},
		func() (provider.TextStream, error) {
			calls.Add(1)
			return testutil.NewMockTextStream(r70Cat(r70Text("1", "recovered"), []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "1"}, r70Stop})), nil
		},
	)
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", StreamRetries: &r70ZeroInt,
		OnError:      func(context.Context, error) { onErrors.Add(1) },
		OnErrorRetry: func(context.Context, StreamTextOnErrorRetryEvent) bool { retryAsks.Add(1); return true }})
	if err != nil {
		t.Fatal(err)
	}
	if got := r70TextDeltas(r70Parts(t, r)); strings.Join(got, "|") != "recovered" {
		t.Fatalf("text deltas = %q", got)
	}
	if calls.Load() != 2 || onErrors.Load() != 1 || retryAsks.Load() != 1 {
		t.Fatalf("calls %d, onError %d, retry asks %d", calls.Load(), onErrors.Load(), retryAsks.Load())
	}
}

// TS 3455: should allow at most one callback-directed retry per logical step.
func TestR70TSStreamAtMostOneCallbackRetry(t *testing.T) {
	var calls atomic.Int32
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		n := calls.Add(1)
		e := fmt.Errorf("provider error %d", n)
		return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeError, Err: e, Text: e.Error()}}), nil
	}}
	var onErrors atomic.Int32
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", StreamRetries: &r70ZeroInt,
		OnError:      func(context.Context, error) { onErrors.Add(1) },
		OnErrorRetry: func(context.Context, StreamTextOnErrorRetryEvent) bool { return calls.Load() <= 2 }})
	if err != nil {
		t.Fatal(err)
	}
	parts := r70Parts(t, r)
	var errTexts []string
	for _, p := range parts {
		if p.Type == provider.ChunkTypeError {
			errTexts = append(errTexts, p.Text)
		}
	}
	if calls.Load() != 2 || onErrors.Load() != 2 {
		t.Fatalf("calls %d, onError %d", calls.Load(), onErrors.Load())
	}
	if strings.Join(errTexts, "|") != "provider error 2" {
		t.Fatalf("error parts = %q, want only provider error 2", errTexts)
	}
	if r.FinishReason() != types.FinishReasonError {
		t.Fatalf("finish reason = %q", r.FinishReason())
	}
}

// TS 3488: should use only recovered transformed content in the next step
// context.
func TestR70TSStreamRetryNextStepContextFromRecoveredTransformedContent(t *testing.T) {
	var mu sync.Mutex
	var nextPrompts []string
	var calls int
	model := &testutil.MockLanguageModel{DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		n := calls
		calls++
		mu.Unlock()
		switch n {
		case 0:
			return testutil.NewMockTextStream(r70Cat(r70Text("failed-text", "partial"), []provider.StreamChunk{r70ErrPart})), nil
		case 1:
			return testutil.NewMockTextStream(r70Cat(r70Text("recovered-text", "recovered"),
				[]provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "recovered-text"}, r70ToolCall("tool-call-1"), r70ToolCallsFinish})), nil
		default:
			b, _ := json.Marshal(o.Prompt.Messages)
			mu.Lock()
			nextPrompts = append(nextPrompts, string(b))
			mu.Unlock()
			return testutil.NewMockTextStream(r70Cat(r70Text("final-text", "done"), []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "final-text"}, r70Stop})), nil
		}
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
		Tools: []types.Tool{r70Tool("tool1", nil)}, StopWhen: []StopCondition{StepCountIs(2)}, StreamRetries: &r70OneRetry,
		ExperimentalTransform: []StreamTransformFunc{func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
			switch c.Type {
			case provider.ChunkTypeTextStart:
				return []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: c.ID, ProviderMetadata: c.ProviderMetadata}}
			case provider.ChunkTypeText:
				c.Text = strings.ToUpper(c.Text)
			}
			return []provider.StreamChunk{c}
		}},
		OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(nextPrompts) != 1 || !strings.Contains(nextPrompts[0], "RECOVERED") || strings.Contains(nextPrompts[0], "partial") || strings.Contains(nextPrompts[0], "PARTIAL") {
		t.Fatalf("next step prompts = %q", nextPrompts)
	}
	steps := r.Steps()
	if len(steps) != 2 || steps[0].Text != "RECOVERED" || steps[1].Text != "DONE" {
		t.Fatalf("steps = %d, texts %q", len(steps), func() []string {
			var s []string
			for _, st := range steps {
				s = append(s, st.Text)
			}
			return s
		}())
	}
}

// TS 3599: should preserve completed steps when retrying a later step.
func TestR70TSStreamRetryPreservesCompletedSteps(t *testing.T) {
	var executed, prepares atomic.Int32
	model := r69Model(
		r70Chunks(r70ToolCall("tool-call-1"), r70ToolCallsFinish),
		r70Chunks(r70ErrPart),
		r70Chunks(r70Cat(r70Text("1", "recovered"), []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "1"}, r70Stop})...),
	)
	var calls atomic.Int32
	inner := model.DoStreamFunc
	model.DoStreamFunc = func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		calls.Add(1)
		return inner(ctx, o)
	}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
		Tools: []types.Tool{r70Tool("tool1", &executed)}, StopWhen: []StopCondition{StepCountIs(2)}, StreamRetries: &r70OneRetry,
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions { prepares.Add(1); return s },
		OnError:     func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	steps := r.Steps()
	if calls.Load() != 3 || executed.Load() != 1 || prepares.Load() != 2 {
		t.Fatalf("calls %d, executed %d, prepareStep %d", calls.Load(), executed.Load(), prepares.Load())
	}
	if len(steps) != 2 || len(steps[0].ToolResults) != 1 || steps[1].Text != "recovered" {
		t.Fatalf("steps = %+v", steps)
	}
}

// TS 3721-ish: should discard tool calls from a failed attempt.
func TestR70TSStreamRetryDiscardsFailedAttemptToolCalls(t *testing.T) {
	var executed atomic.Int32
	model := r69Model(
		r70Chunks(r70ToolCall("failed-tool-call"), r70ErrPart),
		r70Chunks(r70Cat(r70Text("1", "recovered"), []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "1"}, r70Stop})...),
	)
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
		Tools: []types.Tool{r70Tool("tool1", &executed)}, StreamRetries: &r70OneRetry, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	fs := r.FinalStep()
	if executed.Load() != 0 || len(fs.ToolCalls) != 0 || len(fs.ToolResults) != 0 {
		t.Fatalf("executed %d, final step tool calls %v results %v", executed.Load(), fs.ToolCalls, fs.ToolResults)
	}
}

// TS 3721: should execute a repeated tool call id only from the recovered
// attempt.
func TestR70TSStreamRetryRepeatedToolCallIDExecutesOnce(t *testing.T) {
	var executed atomic.Int32
	model := r69Model(
		r70Chunks(r70ToolCall("shared-tool-call"), r70ErrPart),
		r70Chunks(r70ToolCall("shared-tool-call"), r70ToolCallsFinish),
	)
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
		Tools: []types.Tool{r70Tool("tool1", &executed)}, StreamRetries: &r70OneRetry, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	fs := r.FinalStep()
	if executed.Load() != 1 || len(fs.ToolCalls) != 1 || len(fs.ToolResults) != 1 {
		t.Fatalf("executed %d, final step tool calls %d results %d", executed.Load(), len(fs.ToolCalls), len(fs.ToolResults))
	}
}

// TS 3776: should discard tool callbacks and approvals from a failed attempt.
func TestR70TSStreamRetryDiscardsFailedAttemptToolCallbacks(t *testing.T) {
	var starts, deltas, available, approvals atomic.Int32
	model := r69Model(
		r70Chunks(
			provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ID: "failed-tool-call", ToolCall: &types.ToolCall{ID: "failed-tool-call", ToolName: "tool1"}},
			provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: "failed-tool-call", Text: "{}"},
			provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: "failed-tool-call"},
			r70ToolCall("failed-tool-call"), r70ErrPart),
		r70Chunks(r70Cat(r70Text("1", "recovered"), []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "1"}, r70Stop})...),
	)
	tool := types.Tool{Name: "tool1", Parameters: map[string]interface{}{"type": "object"},
		OnInputStart:     func(context.Context, types.OnInputStartOptions) error { starts.Add(1); return nil },
		OnInputDelta:     func(context.Context, types.OnInputDeltaOptions) error { deltas.Add(1); return nil },
		OnInputAvailable: func(context.Context, types.OnInputAvailableOptions) error { available.Add(1); return nil },
	}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{tool},
		ToolApproval: func(types.ToolApprovalOptions) types.ToolApprovalResult {
			approvals.Add(1)
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
		},
		StreamRetries: &r70OneRetry, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if starts.Load()+deltas.Load()+available.Load()+approvals.Load() != 0 {
		t.Fatalf("onInputStart %d, onInputDelta %d, onInputAvailable %d, toolApproval %d; want none",
			starts.Load(), deltas.Load(), available.Load(), approvals.Load())
	}
}

// TS 3845: should scope lifecycle callbacks to logical steps and completed
// model calls.
func TestR70TSStreamRetryLifecycleCallbackScope(t *testing.T) {
	var stepStarts, callStarts, callEnds atomic.Int32
	model := r69Model(
		r70Chunks(r70ErrPart),
		r70Chunks(r70Cat(r70Text("1", "recovered"), []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: "1"}, r70Stop})...),
	)
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", StreamRetries: &r70OneRetry,
		OnStepStart:              func(context.Context, OnStepStartEvent) { stepStarts.Add(1) },
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) { callStarts.Add(1) },
		OnLanguageModelCallEnd:   func(context.Context, LanguageModelCallEndEvent) { callEnds.Add(1) },
		OnError:                  func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if stepStarts.Load() != 1 || callStarts.Load() != 2 || callEnds.Load() != 1 || len(r.Steps()) != 1 {
		t.Fatalf("onStepStart %d, onLanguageModelCallStart %d, onLanguageModelCallEnd %d, steps %d",
			stepStarts.Load(), callStarts.Load(), callEnds.Load(), len(r.Steps()))
	}
}

// TS 3887: should close partial text and reasoning before recovered UI output.
func TestR70TSStreamRetryClosesPartialBlocksInUIMessage(t *testing.T) {
	model := r69Model(
		r70Chunks(
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "failed-text"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "failed-text", Text: "partial text"},
			provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "failed-reasoning"},
			provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: "failed-reasoning", Text: "partial reasoning"},
			r70ErrPart),
		r70Chunks(
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "recovered-text"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "recovered-text", Text: "recovered text"},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "recovered-text"},
			provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "recovered-reasoning"},
			provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: "recovered-reasoning", Text: "recovered reasoning"},
			provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "recovered-reasoning"},
			r70Stop),
	)
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", StreamRetries: &r70OneRetry, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	var final UIMessageChunk
	chunks, errCh := CreateUIMessageStream(context.Background(), r, UIMessageStreamResultOptions{
		OnEnd: func(e map[string]interface{}) { final, _ = e["responseMessage"].(UIMessageChunk) },
	})
	for range chunks {
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	parts, _ := final["parts"].([]interface{})
	type part struct{ typ, id, text, state string }
	var got []part
	for _, p := range parts {
		m, _ := p.(UIMessageChunk)
		got = append(got, part{stringValue(m["type"]), stringValue(m["id"]), stringValue(m["text"]), stringValue(m["state"])})
	}
	want := []part{
		{"step-start", "", "", ""},
		{"text", "", "partial text", "done"},
		{"reasoning", "failed-reasoning", "partial reasoning", "done"},
		{"text", "", "recovered text", "done"},
		{"reasoning", "recovered-reasoning", "recovered reasoning", "done"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UI message parts = %+v\nwant %+v", got, want)
	}
}

// r70ChanStream is a provider stream fed by the test.
type r70ChanStream struct{ ch chan provider.StreamChunk }

func (s *r70ChanStream) Next() (*provider.StreamChunk, error) {
	c, ok := <-s.ch
	if !ok {
		return nil, io.EOF
	}
	return &c, nil
}
func (s *r70ChanStream) Err() error   { return nil }
func (s *r70ChanStream) Close() error { return nil }

// TS 3981 and 4055: tool parts stream incrementally when stream recovery is
// not configured, also with an onError observer.
func TestR70TSStreamToolPartsIncrementalWithoutRecovery(t *testing.T) {
	for _, withOnError := range []bool{false, true} {
		t.Run(fmt.Sprintf("onError=%v", withOnError), func(t *testing.T) {
			src := &r70ChanStream{ch: make(chan provider.StreamChunk, 4)}
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return src, nil
			}}
			opts := StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}}}}
			if withOnError {
				opts.OnError = func(context.Context, error) {}
			}
			r, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			parts := make(chan provider.StreamChunk, 16)
			go func() {
				defer close(parts)
				s := r.FullStream()
				for {
					c, err := s.Next()
					if err != nil {
						return
					}
					parts <- *c
				}
			}()
			readUntil := func(typ provider.ChunkType) provider.StreamChunk {
				t.Helper()
				timeout := time.After(2 * time.Second)
				for {
					select {
					case c, ok := <-parts:
						if !ok {
							t.Fatalf("stream ended before %s", typ)
						}
						if c.Type == typ {
							return c
						}
					case <-timeout:
						t.Fatalf("timed out waiting for %s", typ)
					}
				}
			}
			src.ch <- provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ID: "tool-call-1", ToolCall: &types.ToolCall{ID: "tool-call-1", ToolName: "tool1"}}
			if c := readUntil(provider.ChunkTypeToolInputStart); c.ID != "tool-call-1" {
				t.Fatalf("tool-input-start = %+v", c)
			}
			src.ch <- provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "tool-call-1", ToolName: "tool1", RawArguments: `{"value":"test"}`}}
			if c := readUntil(provider.ChunkTypeToolCall); c.ToolCall == nil || c.ToolCall.ID != "tool-call-1" || c.ToolCall.Arguments["value"] != "test" {
				t.Fatalf("tool-call = %+v", c.ToolCall)
			}
			src.ch <- r70ToolCallsFinish
			close(src.ch)
			for range parts {
			}
		})
	}
}

// TS 4129: should not retry from an existing onError observer without
// streamRetries.
func TestR70TSStreamNoCallbackRetryWithoutStreamRetries(t *testing.T) {
	var calls, onErrors atomic.Int32
	model := r69Model(
		func() (provider.TextStream, error) {
			calls.Add(1)
			return testutil.NewMockTextStream([]provider.StreamChunk{r70ErrPart}), nil
		},
		func() (provider.TextStream, error) {
			calls.Add(1)
			return testutil.NewMockTextStream([]provider.StreamChunk{r70Stop}), nil
		},
	)
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
		OnError:      func(context.Context, error) { onErrors.Add(1) },
		OnErrorRetry: func(context.Context, StreamTextOnErrorRetryEvent) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if calls.Load() != 1 || onErrors.Load() != 1 || r.FinishReason() != types.FinishReasonError {
		t.Fatalf("calls %d, onError %d, finish %q", calls.Load(), onErrors.Load(), r.FinishReason())
	}
}

// TS 4192: should emit the final error after stream retries are exhausted.
func TestR70TSStreamFinalErrorAfterRetriesExhausted(t *testing.T) {
	var calls, onErrors atomic.Int32
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		e := fmt.Errorf("provider error %d", calls.Add(1))
		return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeError, Err: e, Text: e.Error()}}), nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", StreamRetries: &r70OneRetry,
		OnError: func(context.Context, error) { onErrors.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	var errTexts []string
	for _, p := range r70Parts(t, r) {
		if p.Type == provider.ChunkTypeError {
			errTexts = append(errTexts, p.Text)
		}
	}
	if calls.Load() != 2 || onErrors.Load() != 2 || strings.Join(errTexts, "|") != "provider error 2" || r.FinishReason() != types.FinishReasonError {
		t.Fatalf("calls %d, onError %d, error parts %q, finish %q", calls.Load(), onErrors.Load(), errTexts, r.FinishReason())
	}
}

// TS 4223: should emit a normal terminal error when a recovery call fails to
// start.
func TestR70TSStreamRecoveryCallFailsToStart(t *testing.T) {
	var calls atomic.Int32
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		if calls.Add(1) == 1 {
			return testutil.NewMockTextStream(r70Cat(r70Text("failed-text", "partial"), []provider.StreamChunk{r70ToolCall("failed-tool-call"), r70ErrPart})), nil
		}
		return nil, errors.New("recovery startup error")
	}}
	var mu sync.Mutex
	var onErrors []string
	var finish *OnFinishEvent
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
		Tools: []types.Tool{r70Tool("tool1", nil)}, MaxRetries: &r70ZeroInt, StreamRetries: &r70OneRetry,
		OnError:    func(_ context.Context, err error) { mu.Lock(); onErrors = append(onErrors, err.Error()); mu.Unlock() },
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); finish = &e; mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	parts := r70Parts(t, r)
	var errTexts []string
	for _, p := range parts {
		if p.Type == provider.ChunkTypeError {
			errTexts = append(errTexts, p.Text)
		}
		if p.Type == provider.ChunkTypeToolCall && p.ToolCall != nil && p.ToolCall.ID == "failed-tool-call" {
			t.Fatalf("failed-attempt tool call reached the stream")
		}
	}
	if calls.Load() != 2 || strings.Join(errTexts, "|") != "recovery startup error" {
		t.Fatalf("calls %d, error parts %q", calls.Load(), errTexts)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(onErrors, "|") != "provider error|recovery startup error" {
		t.Fatalf("onError calls = %q", onErrors)
	}
	if finish == nil || finish.FinishReason != types.FinishReasonError || finish.Text != "partial" || len(finish.ToolCalls) != 0 || len(finish.ToolResults) != 0 {
		t.Fatalf("onEnd = %+v", finish)
	}
	if r.FinishReason() != types.FinishReasonError || r.Text() != "partial" || len(r.FinalStep().ToolCalls) != 0 {
		t.Fatalf("finish %q, text %q, final step tool calls %v", r.FinishReason(), r.Text(), r.FinalStep().ToolCalls)
	}
}

// TS 4280: should reject invalid streamRetries values.
func TestR70TSStreamRejectsNegativeStreamRetries(t *testing.T) {
	neg := -1
	_, err := StreamText(context.Background(), StreamTextOptions{Model: &testutil.MockLanguageModel{}, Prompt: "test-input", StreamRetries: &neg})
	if err == nil || !strings.Contains(err.Error(), "streamRetries must be >= 0") {
		t.Fatalf("err = %v", err)
	}
}

// TS 4306: should expose the same normalized provider error to onError and
// stream consumers.
func TestR70TSStreamSameProviderErrorForOnErrorAndStream(t *testing.T) {
	status := 529
	provErr := &providererrors.StreamProviderError{Provider: "anthropic.messages", Message: "Overloaded", Type: "overloaded_error", Code: "provider_overloaded", StatusCode: &status, IsRetryable: true}
	model := r69Model(r70Chunks(r70Cat(r70Text("1", "Hello"), []provider.StreamChunk{{Type: provider.ChunkTypeError, Err: provErr, Text: provErr.Error()}})...))
	var onErrorValue error
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
		OnError: func(_ context.Context, err error) { onErrorValue = err }})
	if err != nil {
		t.Fatal(err)
	}
	var partErr error
	for _, p := range r70Parts(t, r) {
		if p.Type == provider.ChunkTypeError {
			partErr = p.Err
			break
		}
	}
	var spe *providererrors.StreamProviderError
	if !errors.As(partErr, &spe) || spe.Message != "Overloaded" || spe.Type != "overloaded_error" || spe.Code != "provider_overloaded" || spe.StatusCode == nil || *spe.StatusCode != 529 || !spe.IsRetryable {
		t.Fatalf("error part error = %#v", partErr)
	}
	if onErrorValue != partErr {
		t.Fatalf("onError got %#v, the error part carries %#v", onErrorValue, partErr)
	}
}

// r70Recorder records user callbacks and telemetry events in order.
type r70Recorder struct {
	telemetry.NoopTelemetryIntegration
	mu         sync.Mutex
	events     []string
	stepErrors []OnStepErrorEvent
	errs       []error
	telStepErr []error
	telErrs    []error
	stepEnds   []types.StepResult
}

func (r *r70Recorder) add(e string) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *r70Recorder) OnStepError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	r.telStepErr = append(r.telStepErr, e.Error)
	r.mu.Unlock()
	r.add("tel:onStepError")
}
func (r *r70Recorder) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	r.telErrs = append(r.telErrs, e.Error)
	r.mu.Unlock()
	r.add("tel:onError")
}
func (r *r70Recorder) OnFinish(context.Context, telemetry.TelemetryFinishEvent) { r.add("tel:onEnd") }
func (r *r70Recorder) OnStepEnd(context.Context, telemetry.TelemetryStepEndEvent) {
	r.add("tel:onStepEnd")
}
func (r *r70Recorder) OnAbort(context.Context, telemetry.TelemetryAbortEvent) { r.add("tel:onAbort") }

func (r *r70Recorder) options(opts StreamTextOptions) StreamTextOptions {
	if opts.Prompt == "" {
		opts.Prompt = "test-input"
	}
	opts.Telemetry = &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{r}}
	opts.OnError = func(_ context.Context, err error) {
		r.mu.Lock()
		r.errs = append(r.errs, err)
		r.mu.Unlock()
		r.add("onError")
	}
	opts.OnStepStart = func(context.Context, OnStepStartEvent) { r.add("onStepStart") }
	opts.OnStepEnd = func(_ context.Context, s types.StepResult, _ interface{}) {
		r.mu.Lock()
		r.stepEnds = append(r.stepEnds, s)
		r.mu.Unlock()
		r.add("onStepEnd")
	}
	opts.OnStepError = func(_ context.Context, e OnStepErrorEvent) {
		r.mu.Lock()
		r.stepErrors = append(r.stepErrors, e)
		r.mu.Unlock()
		r.add("onStepError")
	}
	opts.OnEndEvent = func(context.Context, OnFinishEvent) { r.add("onEnd") }
	opts.OnAbortEvent = func(context.Context, GenerateTextAbortEvent) { r.add("onAbort") }
	return opts
}

func (r *r70Recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.events, " ")
}

func r70Run(t *testing.T, rec *r70Recorder, opts StreamTextOptions) *StreamTextResult {
	t.Helper()
	r, err := StreamText(context.Background(), rec.options(opts))
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	return r
}

var r70NoFinish = "No output generated. The model stream ended without a finish chunk."

func r70ToolCallModelThen(rest ...func() (provider.TextStream, error)) *testutil.MockLanguageModel {
	first := r70Chunks(provider.StreamChunk{Type: provider.ChunkTypeStreamStart}, r70ToolCall("call-1"), r70ToolCallsFinish)
	return r69Model(append([]func() (provider.TextStream, error){first}, rest...)...)
}

// TS 4358 and 4776: a provider failure on a continuation step is that
// step's error part (onChunk, onError) and reaches onStepError for step 1
// (callback, then telemetry). With a step recorded TS then ends the call like
// a finished run without a finish part: onEnd and the telemetry onEnd, no
// telemetry onError, finish reason "other". A later step whose settings fail
// validation (TS streamStep throws before onStepStart) ends the same way,
// without onStepError.
func TestR70TSStreamContinuationStepFailureEndsTheCall(t *testing.T) {
	providerErr := errors.New("provider failure")
	bad := 0
	cases := []struct {
		name      string
		opts      StreamTextOptions
		wantErr   func(error) bool
		wantOrder string
		wantTel   string
		stepError bool
	}{
		{
			name:      "provider call throws",
			opts:      StreamTextOptions{Model: r70ToolCallModelThen(func() (provider.TextStream, error) { return nil, providerErr })},
			wantErr:   func(err error) bool { return errors.Is(err, providerErr) },
			wantOrder: "onStepStart onStepEnd onStepStart chunk:error onError onStepError onEnd",
			wantTel:   "tel:onStepEnd tel:onStepError tel:onEnd",
			stepError: true,
		},
		{
			name: "step settings invalid",
			opts: StreamTextOptions{Model: r70ToolCallModelThen(r70Chunks(r70Stop)),
				PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
					if s.StepNumber == 1 {
						s.MaxOutputTokens = &bad
					}
					return s
				}},
			wantErr: func(err error) bool {
				var ia *providererrors.InvalidArgumentError
				return errors.As(err, &ia)
			},
			wantOrder: "onStepStart onStepEnd chunk:error onError onEnd",
			wantTel:   "tel:onStepEnd tel:onEnd",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &r70Recorder{}
			opts := tc.opts
			opts.Tools = []types.Tool{r70Tool("tool1", nil)}
			opts.MaxRetries = &r70ZeroInt
			opts.StopWhen = []StopCondition{StepCountIs(3)}
			opts.OnChunk = func(c provider.StreamChunk) {
				switch c.Type {
				case provider.ChunkTypeError, provider.ChunkTypeFinish:
					rec.add("chunk:" + string(c.Type))
				}
			}
			r := r70Run(t, rec, opts)
			rec.mu.Lock()
			defer rec.mu.Unlock()
			// User callbacks and telemetry are compared separately: Go
			// notifies the telemetry integration before the callback of the
			// same event (TS runs them together, callback first).
			var user, tel []string
			for _, e := range rec.events {
				if strings.HasPrefix(e, "tel:") {
					tel = append(tel, e)
				} else {
					user = append(user, e)
				}
			}
			if got := strings.Join(user, " "); got != tc.wantOrder {
				t.Fatalf("callbacks = %q\nwant        %q", got, tc.wantOrder)
			}
			if got := strings.Join(tel, " "); got != tc.wantTel {
				t.Fatalf("telemetry = %q\nwant        %q", got, tc.wantTel)
			}
			if len(rec.errs) != 1 || !tc.wantErr(rec.errs[0]) {
				t.Fatalf("onError = %v", rec.errs)
			}
			if tc.stepError {
				if len(rec.stepErrors) != 1 || rec.stepErrors[0].StepNumber != 1 || !errors.Is(rec.stepErrors[0].Error, providerErr) {
					t.Fatalf("onStepError = %+v", rec.stepErrors)
				}
				if len(rec.telStepErr) != 1 || !errors.Is(rec.telStepErr[0], providerErr) {
					t.Fatalf("telemetry onStepError = %v", rec.telStepErr)
				}
			}
			if len(rec.telErrs) != 0 {
				t.Fatalf("telemetry onError = %v, want none", rec.telErrs)
			}
			if r.FinishReason() != types.FinishReasonOther || len(r.Steps()) != 1 || r.Steps()[0].FinishReason != types.FinishReasonToolCalls {
				t.Fatalf("finish %q, %d steps", r.FinishReason(), len(r.Steps()))
			}
			if !tc.wantErr(r.Err()) {
				t.Fatalf("Err() = %v, want the error part's error", r.Err())
			}
		})
	}
}

// ---- toUIMessageStream (round 70) ----

func r70UITypes(chunks <-chan UIMessageChunk) []string {
	var out []string
	for c := range chunks {
		out = append(out, stringValue(c["type"]))
	}
	return out
}

// TS toUIMessageChunk writes the finish chunk for the source's `finish` part
// only: a StreamText run whose later step fails to open ends with that step's
// error part and no finish chunk; the outcome is the error part's failure.
func TestR70UIContinuationFailureWritesNoFinishChunk(t *testing.T) {
	providerErr := errors.New("provider failure")
	r, err := StreamText(context.Background(), StreamTextOptions{Prompt: "test-input",
		Model: r70ToolCallModelThen(func() (provider.TextStream, error) { return nil, providerErr }),
		Tools: []types.Tool{r70Tool("tool1", nil)}, MaxRetries: &r70ZeroInt, StopWhen: []StopCondition{StepCountIs(3)}})
	if err != nil {
		t.Fatal(err)
	}
	var outcome map[string]interface{}
	chunks, errCh := CreateUIMessageStream(context.Background(), r, UIMessageStreamResultOptions{
		OnEnd: func(e map[string]interface{}) { outcome, _ = e["outcome"].(map[string]interface{}) },
	})
	got := strings.Join(r70UITypes(chunks), " ")
	if streamErr := <-errCh; streamErr != nil {
		t.Fatalf("stream error = %v", streamErr)
	}
	if want := "start start-step tool-input-available tool-output-available finish-step error"; got != want {
		t.Fatalf("UI chunks = %q\nwant      %q", got, want)
	}
	if outcome["status"] != "failed" {
		t.Fatalf("outcome = %v", outcome)
	}
	if e, _ := outcome["error"].(error); !errors.Is(e, providerErr) {
		t.Fatalf("outcome error = %v", outcome["error"])
	}
}

// TS streamText ends an aborted run with one `abort` part and a normally
// closing stream: toUIMessageStream writes one abort chunk.
func TestR70UIAbortPartWritesOneAbortChunk(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &r70ChanStream{ch: make(chan provider.StreamChunk, 4)}
	src.ch <- provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"}
	src.ch <- provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "a"}
	r, err := StreamText(ctx, StreamTextOptions{Prompt: "test-input", Model: r69Model(func() (provider.TextStream, error) { return src, nil })})
	if err != nil {
		t.Fatal(err)
	}
	var outcome map[string]interface{}
	chunks, errCh := CreateUIMessageStream(context.Background(), r, UIMessageStreamResultOptions{
		OnEnd: func(e map[string]interface{}) { outcome, _ = e["outcome"].(map[string]interface{}) },
	})
	var types []string
	for c := range chunks {
		types = append(types, stringValue(c["type"]))
		if c["type"] == "text-delta" {
			cancel()
			close(src.ch)
		}
	}
	if streamErr := <-errCh; streamErr != nil {
		t.Fatalf("stream error = %v", streamErr)
	}
	if got, want := strings.Join(types, " "), "start start-step text-start text-delta abort"; got != want {
		t.Fatalf("UI chunks = %q, want %q", got, want)
	}
	if outcome["status"] != "aborted" {
		t.Fatalf("outcome = %v", outcome)
	}
}

// r70SliceStream yields chunks, then fails with err.
type r70SliceStream struct {
	chunks []provider.StreamChunk
	err    error
}

func (s *r70SliceStream) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) == 0 {
		return nil, s.err
	}
	c := s.chunks[0]
	s.chunks = s.chunks[1:]
	return &c, nil
}
func (s *r70SliceStream) Err() error   { return s.err }
func (s *r70SliceStream) Close() error { return nil }

// An error part followed by other parts does not hide a later source failure
// with the same error: TS writes the part's error chunk and, for the failing
// source, a second one (only a failure right after the error part repeats
// it, as StreamText's stream does).
func TestR70UIErrorPartThenSourceFailureWritesBothErrors(t *testing.T) {
	e := errors.New("boom")
	cases := []struct {
		name   string
		chunks []provider.StreamChunk
		want   int
	}{
		{"failure right after the error part", []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "a"}, {Type: provider.ChunkTypeError, Err: e, Text: "boom"}}, 1},
		{"failure after a later part", []provider.StreamChunk{{Type: provider.ChunkTypeError, Err: e, Text: "boom"}, {Type: provider.ChunkTypeText, Text: "a"}}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks, errCh := ToUIMessageStream(context.Background(), &r70SliceStream{chunks: tc.chunks, err: e})
			n := 0
			for _, typ := range r70UITypes(chunks) {
				if typ == "error" {
					n++
				}
			}
			<-errCh
			if n != tc.want {
				t.Fatalf("error chunks = %d, want %d", n, tc.want)
			}
		})
	}
}
