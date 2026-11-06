package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r59FailingStream streams its chunks and then fails, like a provider body
// that breaks mid-response (TS controller.error on the model stream).
type r59FailingStream struct {
	chunks []provider.StreamChunk
	err    error
}

func (s *r59FailingStream) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) == 0 {
		return nil, s.err
	}
	c := s.chunks[0]
	s.chunks = s.chunks[1:]
	return &c, nil
}
func (s *r59FailingStream) Err() error   { return s.err }
func (s *r59FailingStream) Close() error { return nil }

var _ provider.TextStream = (*r59FailingStream)(nil)

// TS streamText: a model stream that throws (not an error part) is caught by
// the outer stream's pull, which reports onStepError and the telemetry
// onError and errors the stream. The onError callback is called only for
// error parts (the event processor), so it does not see this error.
func TestR59StreamTextProviderStreamThrowSkipsOnError(t *testing.T) {
	providerErr := errors.New("provider stream failure")
	var mu sync.Mutex
	var onErrors, stepErrors int
	rec := &r59AbortTelemetry{}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt: "a",
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &r59FailingStream{chunks: []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}}, err: providerErr}, nil
		}},
		OnError: func(context.Context, error) { mu.Lock(); onErrors++; mu.Unlock() },
		OnStepError: func(context.Context, OnStepErrorEvent) {
			mu.Lock()
			stepErrors++
			mu.Unlock()
		},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := res.FullStream()
	var streamErr error
	for {
		_, err := s.Next()
		if err != nil {
			if err != io.EOF {
				streamErr = err
			}
			break
		}
	}
	if !errors.Is(streamErr, providerErr) && !errors.Is(res.Err(), providerErr) {
		t.Fatalf("stream err = %v, result err = %v; want the provider error", streamErr, res.Err())
	}
	mu.Lock()
	defer mu.Unlock()
	if onErrors != 0 || stepErrors != 1 {
		t.Fatalf("OnError calls = %d, OnStepError calls = %d; want 0 and 1", onErrors, stepErrors)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.errors != 1 {
		t.Fatalf("telemetry OnError calls = %d, want 1", rec.errors)
	}
}

// The same holds for the other failures TS throws inside its stream: a file
// download and a tool input callback.
func TestR59StreamTextThrownFailuresSkipOnError(t *testing.T) {
	inputErr := errors.New("input callback failed")
	var hits atomic.Int32
	toolCall := func() provider.TextStream {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "1", ToolName: "t"}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		})
	}
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return toolCall(), nil
	}}
	cases := map[string]StreamTextOptions{
		"tool input callback": {Prompt: "a", Model: model, Tools: []types.Tool{{Name: "t",
			OnInputStart: func(context.Context, types.OnInputStartOptions) error { return inputErr },
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "r", nil
			},
		}}},
		"file download": {Prompt: "a", Model: fileModel(urlFile(fileServer(t, http.StatusServiceUnavailable, &hits)))},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			opts.OnError = func(context.Context, error) { mu.Lock(); calls++; mu.Unlock() }
			res, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = r58FullStreamParts(t, res)
			if res.Err() == nil {
				t.Fatal("want the run to fail")
			}
			mu.Lock()
			defer mu.Unlock()
			if calls != 0 {
				t.Fatalf("OnError calls = %d, want 0 (%v)", calls, res.Err())
			}
		})
	}
}
