package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r58AbortRecorder struct {
	mu     sync.Mutex
	errs   []error
	aborts []GenerateTextAbortEvent
}

func (r *r58AbortRecorder) wire(opts *StreamTextOptions) {
	opts.OnError = func(_ context.Context, err error) { r.mu.Lock(); r.errs = append(r.errs, err); r.mu.Unlock() }
	opts.OnAbortEvent = func(_ context.Context, e GenerateTextAbortEvent) {
		r.mu.Lock()
		r.aborts = append(r.aborts, e)
		r.mu.Unlock()
	}
}

func r58FullStreamParts(t *testing.T, res *StreamTextResult) (string, []provider.StreamChunk) {
	t.Helper()
	var parts []string
	var chunks []provider.StreamChunk
	s := res.FullStream()
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		parts = append(parts, string(c.Type))
		chunks = append(chunks, *c)
	}
	return strings.Join(parts, " "), chunks
}

// TS streamText: a pre-stream failure is caught by the pipeline, which adds
// an `error` part; the outer stream reads it, sees the fired abort signal and
// calls abort() instead: `start`, onAbort and an `abort` part, with no error
// part and no onError, whatever the thrown error was. Go dropped both and
// emitted nothing.
func TestR58StreamTextAbortBeforeFirstStreamIsAnAbortPart(t *testing.T) {
	cases := map[string]func(cancel context.CancelFunc) StreamTextOptions{
		"PrepareStep instructions (non-abort error)": func(cancel context.CancelFunc) StreamTextOptions {
			return StreamTextOptions{Prompt: "a", PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions {
				cancel()
				return PrepareStepOptions{InstructionMessages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}}}
			}}
		},
		"PrepareStep maxOutputTokens (non-abort error)": func(cancel context.CancelFunc) StreamTextOptions {
			return StreamTextOptions{Prompt: "a", PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions {
				cancel()
				zero := 0
				return PrepareStepOptions{MaxOutputTokens: &zero}
			}}
		},
		"model call fails to open": func(cancel context.CancelFunc) StreamTextOptions {
			return StreamTextOptions{Prompt: "a", Model: &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
				cancel()
				return nil, ctx.Err()
			}}}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			stop := func() { cancel(errors.New("stopped by caller")) }
			opts := build(stop)
			if opts.Model == nil {
				opts.Model = &testutil.MockLanguageModel{}
			}
			var rec r58AbortRecorder
			rec.wire(&opts)
			res, err := StreamText(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			parts, chunks := r58FullStreamParts(t, res)
			if res.Err() == nil {
				t.Fatal("want the run to fail")
			}
			if parts != "start abort" {
				t.Fatalf("full stream = %q, want %q", parts, "start abort")
			}
			if reason := chunks[1].AbortReason; reason != "stopped by caller" {
				t.Fatalf("abort reason = %q, want the cancel cause", reason)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.errs) != 0 {
				t.Fatalf("OnError = %v, want no call", rec.errs)
			}
			if len(rec.aborts) != 1 || rec.aborts[0].CallID == "" {
				t.Fatalf("OnAbort = %+v, want one call with the call ID", rec.aborts)
			}
		})
	}
}

// TS: when the next streamStep throws after the abort signal fired, its
// `error` part is read by the outer stream, which ends with abort() instead,
// so the full stream ends with an `abort` part and onError is not called.
func TestR58StreamTextAbortWhilePreparingNextStepIsAnAbortPart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}}
	opts := StreamTextOptions{
		Model:    model,
		Prompt:   "a",
		StopWhen: []StopCondition{IsStepCount(3)},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			if o.StepNumber == 1 {
				cancel()
				zero := 0
				return PrepareStepOptions{MaxOutputTokens: &zero}
			}
			return PrepareStepOptions{}
		},
	}
	var rec r58AbortRecorder
	rec.wire(&opts)
	res, err := StreamText(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := r58FullStreamParts(t, res)
	if res.Err() == nil {
		t.Fatal("want the run to fail")
	}
	if strings.Contains(parts, string(provider.ChunkTypeError)) || !strings.HasSuffix(parts, " abort") {
		t.Fatalf("full stream = %q, want it to end with abort and carry no error part", parts)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.errs) != 0 {
		t.Fatalf("OnError = %v, want no call", rec.errs)
	}
	if len(rec.aborts) != 1 {
		t.Fatalf("OnAbort calls = %d, want 1", len(rec.aborts))
	}
}
