package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
