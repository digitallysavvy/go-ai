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
