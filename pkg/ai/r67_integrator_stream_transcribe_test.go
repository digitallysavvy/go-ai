package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR67IntegratorStreamTranscribeCloseDuringDoStream: a FullStream closed
// while the provider is still opening its stream rejects the result with the
// TS cancel reason (pipeTo rejects with the abort reason), not the provider's
// context error.
func TestR67IntegratorStreamTranscribeCloseDuringDoStream(t *testing.T) {
	entered := make(chan struct{})
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, _ *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{Model: model, Audio: newMockChanAudioStream()})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := result.FullStream()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("DoStream not called")
	}
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
}

// TestR67IntegratorStreamTranscribeCallerCancelWhileSendBlocked: a caller
// ctx cancelled while the pipe is blocked sending a part rejects the result
// with the ctx's cause (the abort reason), not the FullStream cancel text.
func TestR67IntegratorStreamTranscribeCallerCancelWhileSendBlocked(t *testing.T) {
	cause := errors.New("caller stopped")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, _ *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			parts := make(chan provider.TranscriptionStreamPart, 3)
			parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "Hel"}
			parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "lo"}
			parts <- provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: "item-1", Delta: "!"}
			return &provider.TranscriptionStreamResult{Stream: &liveProviderTranscriptionStream{ctx: ctx, parts: parts}}, nil
		},
	}
	result, err := ExperimentalStreamTranscribe(ctx, StreamTranscribeOptions{Model: model, Audio: newMockChanAudioStream()})
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
	cancel(cause)
	done := make(chan error, 1)
	go func() { _, err := result.Text(); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, cause) {
			t.Fatalf("Text() error = %v, want the caller ctx cause", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Text() did not settle after the caller cancelled")
	}
}
