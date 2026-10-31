package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r56BlockingStream yields its chunks, then blocks until ctx ends (calling
// onBlock first) and returns the context error, as a provider read does.
type r56BlockingStream struct {
	ctx     context.Context
	chunks  []provider.StreamChunk
	onBlock func()
	err     error
}

func (s *r56BlockingStream) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) > 0 {
		c := s.chunks[0]
		s.chunks = s.chunks[1:]
		return &c, nil
	}
	if s.onBlock != nil {
		s.onBlock()
		s.onBlock = nil
	}
	<-s.ctx.Done()
	s.err = s.ctx.Err()
	return nil, s.err
}

func (s *r56BlockingStream) Err() error   { return s.err }
func (s *r56BlockingStream) Close() error { return nil }

func r56StreamErr(t *testing.T, ctx context.Context, opts StreamTextOptions) error {
	t.Helper()
	result, err := StreamText(ctx, opts)
	if err != nil {
		return err
	}
	if _, err := result.ReadAll(); err != nil {
		return err
	}
	return result.Err()
}

func r56ToolCallStream() provider.TextStream {
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	})
}

func assertTimeoutReason(t *testing.T, err error, want TimeoutReason) {
	t.Helper()
	var te *TimeoutError
	if !errors.As(err, &te) || te.Reason != want {
		t.Fatalf("err = %v, want a %s TimeoutError", err, want)
	}
}

// r56 integrator: every StreamText (and the GenerateText tool) classification
// site tells the SDK's deadlines from a caller cancellation.
func TestR56IntegratorTimeoutClassificationSites(t *testing.T) {
	short := 30 * time.Millisecond
	long := time.Hour
	longBoth := &TimeoutConfig{Total: &long, PerStep: &long}
	totalShort := &TimeoutConfig{Total: &short, PerStep: &long}

	t.Run("stream read: total deadline is a total timeout", func(t *testing.T) {
		model := &testutil.MockLanguageModel{DoStreamFunc: func(c context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return &r56BlockingStream{ctx: c, chunks: []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "a"}}}, nil
		}}
		assertTimeoutReason(t, r56StreamErr(t, context.Background(), StreamTextOptions{Model: model, Prompt: "hi", Timeout: totalShort}), TimeoutReasonTotal)
	})

	t.Run("stream read: caller cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		model := &testutil.MockLanguageModel{DoStreamFunc: func(c context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return &r56BlockingStream{ctx: c, chunks: []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "a"}}, onBlock: cancel}, nil
		}}
		assertCallerCancel(t, r56StreamErr(t, ctx, StreamTextOptions{Model: model, Prompt: "hi", Timeout: longBoth}))
	})

	toolLoop := func(execute func(context.Context) (interface{}, error), timeout *TimeoutConfig, ctx context.Context, stream bool, queued bool) error {
		tool := types.Tool{
			Name:       "t",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(c context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				return execute(c)
			},
		}
		calls := []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}}
		var concurrency *int
		if queued {
			// A second call queued behind the first never starts once the
			// caller cancels, so tool execution itself returns the abort.
			one := 1
			concurrency = &one
			calls = append(calls, types.ToolCall{ID: "c2", ToolName: "t", Arguments: map[string]interface{}{}})
		}
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				chunks := make([]provider.StreamChunk, 0, len(calls)+1)
				for i := range calls {
					chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &calls[i]})
				}
				chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls})
				return testutil.NewMockTextStream(chunks), nil
			},
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls}, nil
			},
		}
		if stream {
			return r56StreamErr(t, ctx, StreamTextOptions{Model: model, Prompt: "hi", Timeout: timeout, Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsStepCount(5)}, ToolCallConcurrency: concurrency})
		}
		_, err := GenerateText(ctx, GenerateTextOptions{Model: model, Prompt: "hi", Timeout: timeout, Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsStepCount(5)}, ToolCallConcurrency: concurrency})
		return err
	}

	for _, stream := range []bool{false, true} {
		mode := map[bool]string{false: "generate", true: "stream"}[stream]
		t.Run(mode+" queued tool call aborted by caller cancel", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := toolLoop(func(c context.Context) (interface{}, error) {
				cancel()
				<-c.Done()
				return nil, c.Err()
			}, longBoth, ctx, stream, true)
			assertCallerCancel(t, err)
		})
	}

	t.Run("stream step end: caller cancel while a tool ignores it", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := toolLoop(func(context.Context) (interface{}, error) {
			cancel()
			return "ok", nil
		}, longBoth, ctx, true, false)
		assertCallerCancel(t, err)
	})

	t.Run("stream step end: total deadline while a tool ignores it", func(t *testing.T) {
		err := toolLoop(func(c context.Context) (interface{}, error) {
			<-c.Done()
			return "ok", nil
		}, totalShort, context.Background(), true, false)
		assertTimeoutReason(t, err, TimeoutReasonTotal)
	})

	t.Run("stream next step open: caller cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		model := &testutil.MockLanguageModel{DoStreamFunc: func(c context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 1 {
				return r56ToolCallStream(), nil
			}
			cancel()
			<-c.Done()
			return nil, c.Err()
		}}
		tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}
		err := r56StreamErr(t, ctx, StreamTextOptions{Model: model, Prompt: "hi", Timeout: longBoth, Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsStepCount(5)}})
		assertCallerCancel(t, err)
		if calls != 2 {
			t.Fatalf("model calls = %d, want 2", calls)
		}
	})
}
