package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR67StreamTranscribeCloseRejectsWithCancelReason mirrors TS
// stream-transcribe.ts Transformer.cancel: cancelling fullStream aborts the
// pipe with Error('Transcription stream was cancelled.'), and that reason
// rejects the pending result promises, whether the pipe is waiting on the
// provider stream or blocked sending a part.
func TestR67StreamTranscribeCloseRejectsWithCancelReason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parts int
	}{
		{"waiting on provider", 0},
		{"blocked on send", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &mockTranscriptionStreamerModel{
				doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
					parts := make(chan provider.TranscriptionStreamPart, 3)
					parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "Hel"}
					for i := 0; i < tc.parts; i++ {
						parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "lo"}
					}
					return &provider.TranscriptionStreamResult{
						Stream: &liveProviderTranscriptionStream{ctx: ctx, parts: parts},
					}, nil
				},
			}
			result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
				Model: model,
				Audio: newMockChanAudioStream(),
			})
			if err != nil {
				t.Fatal(err)
			}
			stream, err := result.FullStream()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Next(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
			_ = stream.Close()
			done := make(chan error, 1)
			go func() { _, err := result.Text(); done <- err }()
			select {
			case err := <-done:
				if err == nil || err.Error() != "Transcription stream was cancelled." {
					t.Fatalf("Text() error = %v, want the TS cancel reason", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Text() did not settle after Close")
			}
		})
	}
}

// TS processUIMessageStream passes the invocation's own title for
// tool-output-available and tool-output-error (toolInvocation.title); the
// chunk schema has no title, so a chunk's title is not applied.
func TestR67ToolOutputChunksKeepPartTitle(t *testing.T) {
	for _, out := range []UIMessageChunk{
		{"type": "tool-output-available", "toolCallId": "c1", "output": 1, "title": "Other"},
		{"type": "tool-output-error", "toolCallId": "c1", "errorText": "bad", "title": "Other"},
	} {
		s := newUIMessageCallbackState(nil, "m")
		s.apply(UIMessageChunk{"type": "tool-input-available", "toolCallId": "c1", "toolName": "w", "input": map[string]interface{}{}, "title": "Own"}, nil)
		s.apply(out, nil)
		if title := asUIPartChunk(uiParts(s.message)[0])["title"]; title != "Own" {
			t.Errorf("%s: title = %v, want Own", out["type"], title)
		}
	}
}

// TS reasoning-start sets `id: chunk.id`, so an empty id is kept on the part.
func TestR67ReasoningStartEmptyIDKept(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "reasoning-start", "id": ""}, nil)
	if id, ok := asUIPartChunk(uiParts(s.message)[0])["id"]; !ok || id != "" {
		t.Fatalf("reasoning part id = %v (present %v), want empty string", id, ok)
	}
}

// TS finish sets finishReason only when `chunk.finishReason != null`.
func TestR67FinishNullReasonKeepsEarlierReason(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "finish", "finishReason": "stop"}, nil)
	s.apply(UIMessageChunk{"type": "finish", "finishReason": nil}, nil)
	if s.finishReason != "stop" {
		t.Fatalf("finishReason = %v, want stop", s.finishReason)
	}
}

// TS tool-input-delta input is parsePartialJson(text).value, undefined for
// text that cannot be parsed or repaired, so the part has no input key.
func TestR67ToolInputDeltaUnparseableInputAbsent(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "tool-input-start", "toolCallId": "c1", "toolName": "w"}, nil)
	s.apply(UIMessageChunk{"type": "tool-input-delta", "toolCallId": "c1", "inputTextDelta": ""}, nil)
	part := asUIPartChunk(uiParts(s.message)[0])
	if v, ok := part["input"]; ok {
		t.Errorf("empty text: input = %v, want absent", v)
	}
	s.apply(UIMessageChunk{"type": "tool-input-delta", "toolCallId": "c1", "inputTextDelta": "null"}, nil)
	if v, ok := part["input"]; !ok || v != nil {
		t.Errorf("null text: input = %v (present %v), want JSON null", v, ok)
	}
}

// TS to-ui-message-stream.test.ts "reports message metadata failures as
// failed exactly once" and "reports UI chunk conversion failures as failed
// exactly once": a messageMetadata or onError callback that throws errors the
// stream with that error, and onEnd runs once with a failed outcome carrying
// it. A Go callback that panics is the thrown error.
func TestR67ToUIMessageStreamCallbackPanicFailsOutcome(t *testing.T) {
	metadataErr := errors.New("message metadata failed")
	conversionErr := errors.New("UI chunk conversion failed")
	for _, tc := range []struct {
		name   string
		chunks []provider.StreamChunk
		opts   UIMessageStreamResultOptions
		want   error
	}{
		{
			name:   "message metadata",
			chunks: []provider.StreamChunk{{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}},
			opts: UIMessageStreamResultOptions{MessageMetadata: func(map[string]interface{}) map[string]interface{} {
				panic(metadataErr)
			}},
			want: metadataErr,
		},
		{
			name:   "conversion",
			chunks: []provider.StreamChunk{{Type: provider.ChunkTypeError, Err: errors.New("generation failed")}},
			opts:   UIMessageStreamResultOptions{OnError: func(error) string { panic(conversionErr) }},
			want:   conversionErr,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var events []map[string]interface{}
			tc.opts.OnEnd = func(e map[string]interface{}) {
				mu.Lock()
				events = append(events, e)
				mu.Unlock()
			}
			out, errCh := ToUIMessageStream(context.Background(), SimulateReadableStream(tc.chunks, 0), tc.opts)
			for range out { //nolint:revive // drain
			}
			if err := <-errCh; !errors.Is(err, tc.want) {
				t.Fatalf("stream error = %v, want %v", err, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(events) != 1 {
				t.Fatalf("onEnd calls = %d, want 1", len(events))
			}
			outcome, _ := events[0]["outcome"].(map[string]interface{})
			if outcome["status"] != "failed" || !errors.Is(outcome["error"].(error), tc.want) {
				t.Fatalf("outcome = %v, want failed with %v", outcome, tc.want)
			}
		})
	}
}

// TS streamText puts the model call's own error in the `error` part of a
// failed step open (the pipeline and step catches enqueue `{ type: 'error',
// error }`); only onError gets wrapGatewayError(part.error).
func TestR67StreamOpenFailureErrorPartCarriesRawError(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	const wrapped = "Unauthenticated. Configure AI_GATEWAY_API_KEY or use a provider module. Learn more: https://ai-sdk.dev/unauthenticated-ai-gateway"
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("step %d", failAt), func(t *testing.T) {
			rawErr := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
			var mu sync.Mutex
			calls := 0
			zero := 0
			model := &testutil.MockLanguageModel{
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					mu.Lock()
					calls++
					n := calls
					mu.Unlock()
					if n == failAt {
						return nil, rawErr
					}
					return SimulateReadableStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}, 0), nil
				},
			}
			var onErrorErr error
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model:      model,
				Prompt:     "hi",
				MaxRetries: &zero,
				StopWhen:   []StopCondition{StepCountIs(3)},
				Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "ok", nil
				}}},
				OnError: func(_ context.Context, err error) {
					mu.Lock()
					onErrorErr = err
					mu.Unlock()
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			stream := result.Stream()
			var errorChunk *provider.StreamChunk
			for {
				c, err := stream.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeError {
					errorChunk = c
				}
			}
			if errorChunk == nil {
				t.Fatal("no error part")
			}
			if errorChunk.Err != error(rawErr) || errorChunk.Text != rawErr.Error() {
				t.Errorf("error part = %T %q, want the model call's own error", errorChunk.Err, errorChunk.Text)
			}
			mu.Lock()
			defer mu.Unlock()
			if onErrorErr == nil || onErrorErr.Error() != wrapped {
				t.Errorf("OnError error = %v, want the wrapped gateway error", onErrorErr)
			}
		})
	}
}
