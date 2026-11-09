package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 90 (stream).

// r90streamModel returns a mock model whose n-th DoStream call returns
// streams[n](ctx) (the last one repeats).
func r90streamModel(streams ...func(ctx context.Context) provider.TextStream) *testutil.MockLanguageModel {
	var mu sync.Mutex
	calls := 0
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		i := calls
		calls++
		mu.Unlock()
		if i >= len(streams) {
			i = len(streams) - 1
		}
		return streams[i](ctx), nil
	}}
}

// r90streamBlocking streams its chunks, then blocks until ctx ends.
type r90streamBlocking struct {
	ctx    context.Context
	chunks []provider.StreamChunk
}

func (s *r90streamBlocking) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) > 0 {
		c := s.chunks[0]
		s.chunks = s.chunks[1:]
		return &c, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}
func (s *r90streamBlocking) Err() error   { return nil }
func (s *r90streamBlocking) Close() error { return nil }

var r90streamTextStart = []provider.StreamChunk{
	{Type: provider.ChunkTypeStreamStart},
	{Type: provider.ChunkTypeTextStart, ID: "1"},
	{Type: provider.ChunkTypeText, ID: "1", Text: "Hi"},
}

func r90streamToolStep(context.Context) provider.TextStream {
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r71streamUsage},
	})
}

var r90streamTools = []types.Tool{{
	Name:       "t",
	Parameters: map[string]interface{}{"type": "object"},
	Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	},
}}

// stream-text.ts consumeStream / cancelOnAbort: a stream that errors (a
// thrown error, not an error part) or is aborted rejects every result
// promise, result.output among them, with that error or the abort reason,
// whether or not a step was recorded. TS output (node, packages/ai/dist at
// 9f7a842ae1): case1 "boom", case2 "boom2", case3 "AbortError: This
// operation was aborted".
func TestR90streamOutputErrAfterStreamFailure(t *testing.T) {
	boom := errors.New("boom")
	t.Run("thrown error in the first step", func(t *testing.T) {
		r, err := StreamText(context.Background(), StreamTextOptions{
			Model: r90streamModel(func(context.Context) provider.TextStream {
				return &r73streamErrAfter{chunks: r90streamTextStart, err: boom}
			}),
			Prompt:  "x",
			OnError: func(context.Context, error) {},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		if got := r.OutputErr(); !errors.Is(got, boom) {
			t.Fatalf("OutputErr = %v, want boom", got)
		}
		if r.Output() != nil {
			t.Fatalf("Output = %v", r.Output())
		}
	})
	t.Run("thrown error in a later step", func(t *testing.T) {
		boom2 := errors.New("boom2")
		r, err := StreamText(context.Background(), StreamTextOptions{
			Model: r90streamModel(r90streamToolStep, func(context.Context) provider.TextStream {
				return &r73streamErrAfter{chunks: r90streamTextStart, err: boom2}
			}),
			Tools:    r90streamTools,
			StopWhen: []StopCondition{StepCountIs(3)},
			Prompt:   "x",
			OnError:  func(context.Context, error) {},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		if got := r.OutputErr(); !errors.Is(got, boom2) {
			t.Fatalf("OutputErr = %v, want boom2", got)
		}
		if r.Output() != nil {
			t.Fatalf("Output = %v", r.Output())
		}
	})
	t.Run("abort in a later step", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r, err := StreamText(ctx, StreamTextOptions{
			Model: r90streamModel(r90streamToolStep, func(ctx context.Context) provider.TextStream {
				time.AfterFunc(20*time.Millisecond, cancel)
				return &r90streamBlocking{ctx: ctx, chunks: r90streamTextStart}
			}),
			Tools:    r90streamTools,
			StopWhen: []StopCondition{StepCountIs(3)},
			Prompt:   "x",
			OnError:  func(context.Context, error) {},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		if got := r.OutputErr(); !errors.Is(got, context.Canceled) {
			t.Fatalf("OutputErr = %v, want context.Canceled", got)
		}
		if r.Output() != nil {
			t.Fatalf("Output = %v", r.Output())
		}
	})
}

// harness-stream-text-result.ts fail / abort reject every delayed promise
// with the error; `.output` is finalStep.then(...), so it rejects with it.
func TestR90streamExternalOutputErrAfterFailure(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name  string
		last  provider.StreamChunk
		check func(error) bool
	}{
		{"error", provider.StreamChunk{Type: provider.ChunkTypeError, Err: boom, Text: "boom"}, func(err error) bool { return errors.Is(err, boom) }},
		{"abort", provider.StreamChunk{Type: provider.ChunkTypeAbort}, func(err error) bool { return errors.Is(err, context.Canceled) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := NewStreamTextResultFromParts(context.Background(), testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: `{"a":1}`},
				c.last,
			}), ExternalStreamOptions{TerminalFinishClosesTurn: true, Output: JSONOutput(JSONOutputOptions{})})
			if err := r.Err(); !c.check(err) {
				t.Fatalf("Err = %v", err)
			}
			if got := r.OutputErr(); !c.check(got) {
				t.Fatalf("OutputErr = %v", got)
			}
			if r.Output() != nil {
				t.Fatalf("Output = %v", r.Output())
			}
		})
	}
}

// to-ui-message-stream.ts 122-133: messageMetadata is called for a part
// before toUIMessageChunk, which calls onError for an error part. TS order
// (node, packages/ai/dist at 9f7a842ae1): meta:start, meta:start-step,
// meta:text-start, meta:text-delta, meta:error, onError:E1, meta:text-end,
// meta:finish-step, meta:finish.
func TestR90streamUIMessageMetadataBeforeOnError(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "a"},
			{Type: provider.ChunkTypeError, Err: errors.New("E1"), Text: "E1"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
		}),
		Prompt:  "x",
		OnError: func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var log []string
	add := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		log = append(log, s)
	}
	chunks, errs := r.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{
		MessageMetadata: func(part map[string]interface{}) map[string]interface{} {
			add("meta:" + part["type"].(string))
			return nil
		},
		OnError: func(err error) string {
			add("onError:" + err.Error())
			return "masked"
		},
	})
	for range chunks {
	}
	for range errs {
	}
	mu.Lock()
	defer mu.Unlock()
	got := strings.Join(log, ",")
	want := "meta:start,meta:start-step,meta:text-start,meta:text-delta,meta:error,onError:E1,meta:text-end,meta:finish-step,meta:finish"
	if got != want {
		t.Fatalf("callback order\n got %s\nwant %s", got, want)
	}
}
