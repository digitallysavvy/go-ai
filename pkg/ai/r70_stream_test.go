package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
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
