package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// closeTrackingStream is a provider.TextStream that fails mid-stream (a raw
// Next() error, e.g. a dropped connection) after emitting a few chunks, and
// records how many times Close() was called.
type closeTrackingStream struct {
	mu         sync.Mutex
	chunks     []provider.StreamChunk
	idx        int
	failErr    error
	closeCalls int
}

func (s *closeTrackingStream) Next() (*provider.StreamChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.chunks) {
		return nil, s.failErr
	}
	c := s.chunks[s.idx]
	s.idx++
	return &c, nil
}

func (s *closeTrackingStream) Err() error { return nil }

func (s *closeTrackingStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCalls++
	return nil
}

func (s *closeTrackingStream) closedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCalls
}

// TestStreamText_ClosesTextStreamOnMidStreamError verifies that when the
// provider stream itself errors mid-stream (not a ChunkTypeError chunk, but
// a raw Next() error such as a dropped connection), processStream closes
// the underlying TextStream itself instead of relying on the caller to call
// StreamTextResult.Close() (hand-off: "processStream closing the TextStream
// on error" — avoids leaking the provider's underlying HTTP response body).
func TestStreamText_ClosesTextStreamOnMidStreamError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection reset")
	stream := &closeTrackingStream{
		chunks: []provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "partial"},
		},
		failErr: wantErr,
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return stream, nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	// ReadAll blocks until processStream's background goroutine finishes
	// (StreamTextResult.processingDone), regardless of whether it ended in
	// an error.
	readAllDone := make(chan struct{})
	go func() {
		defer close(readAllDone)
		_, _ = result.ReadAll()
	}()
	select {
	case <-readAllDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the stream to finish")
	}

	if result.Err() == nil {
		t.Fatal("expected StreamTextResult.Err() to be non-nil after a mid-stream Next() error")
	}
	if !errors.Is(result.Err(), wantErr) && result.Err().Error() == "" {
		// Not asserting exact wrapping, just that an error surfaced.
		t.Fatalf("expected an error, got %v", result.Err())
	}
	if got := stream.closedCount(); got < 1 {
		t.Fatalf("expected the TextStream to be closed by processStream on error, closeCalls = %d", got)
	}
}
