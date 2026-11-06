package langchain

import (
	"context"
	"testing"
)

// TS toUIMessageStream: an abort calls onAbort only. Without an onAbort
// callback, onError must not be called with the cancellation.
func TestR30CancelledStreamDoesNotCallOnErrorWithoutOnAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan StreamEvent)
	errored := false
	chunks, errs := ToUIMessageStreamWithCallbacks(ctx, events, &StreamCallbacks{
		OnError: func(error) error { errored = true; return nil },
	})
	cancel()
	for range chunks {
	}
	for range errs {
	}
	if errored {
		t.Fatal("OnError was called for a cancelled stream")
	}
}
