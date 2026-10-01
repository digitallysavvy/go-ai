package ai

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// blockingCloseAwareStream is a minimal provider.TextStream that models the
// contract real implementations rely on (e.g. OpenAICompatStream, which
// defers Close to the underlying http.Response.Body): a call to Close()
// promptly unblocks any goroutine currently parked inside Next(), the same
// way closing an io.ReadCloser unblocks a concurrent blocked Read. Next()
// otherwise "hangs" far longer than any per-chunk timeout under test, like a
// stalled connection with no OS-level read deadline.
type blockingCloseAwareStream struct {
	mu      sync.Mutex
	closed  bool
	closeCh chan struct{}
}

func newBlockingCloseAwareStream() *blockingCloseAwareStream {
	return &blockingCloseAwareStream{closeCh: make(chan struct{})}
}

func (s *blockingCloseAwareStream) Next() (*provider.StreamChunk, error) {
	select {
	case <-s.closeCh:
		return nil, io.ErrClosedPipe
	case <-time.After(10 * time.Second):
		// Never actually reached in this test: either the per-chunk
		// timeout abandons this call, or Close() unblocks it first.
		return &provider.StreamChunk{Type: provider.ChunkTypeText, Text: "late"}, nil
	}
}

func (s *blockingCloseAwareStream) Err() error { return nil }

func (s *blockingCloseAwareStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.closeCh)
	}
	return nil
}

// TestNextChunk_AbandonedGoroutineDoesNotRaceClose addresses the third R1
// "Unverified" item (bug-review/R1.md): nextChunk's per-chunk-timeout path
// abandons a goroutine still blocked in stream.Next() when the timeout
// fires, and processStream (or a consumer via StreamTextResult.Close) may
// call stream.Close() shortly after on that same stream object while that
// goroutine is still in flight.
//
// This is not a new or special risk: StreamTextResult.Close is already
// documented and designed to run concurrently with an in-flight Next (see
// nextChunk's own doc comment and Close's doc comment on currentStream/
// setStream) for the ordinary non-timeout case too -- a consumer may call
// Close at any time while processStream's main loop is itself blocked
// inside a live stream.Next() call, with no timeout involved at all. Both
// cases rely on the same contract every provider.TextStream implementation
// embedding the shared OpenAICompatStream base fulfills: Close() closes the
// underlying io.ReadCloser, which unblocks a concurrently blocked Read/Next
// the same way closing an http.Response.Body does -- not a new, separate
// guarantee invented for timeouts.
//
// This test proves pkg/ai's own orchestration (nextChunk/setStream/
// currentStream/Close, all synchronized via StreamTextResult.mu for the
// r.stream field itself) introduces no data race when paired with a
// contract-abiding stream implementation: the abandoned goroutine's call
// into Next() and the main goroutine's call into Close() run concurrently
// by design, and -race must stay clean.
func TestNextChunk_AbandonedGoroutineDoesNotRaceClose(t *testing.T) {
	stream := newBlockingCloseAwareStream()
	r := &StreamTextResult{}
	perChunk := 20 * time.Millisecond
	r.timeout = &TimeoutConfig{PerChunk: &perChunk}
	r.setStream(stream)

	_, err := r.nextChunk(context.Background())
	if err == nil {
		t.Fatal("expected a per-chunk timeout error")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonChunk {
		t.Fatalf("nextChunk error = %v, want a TimeoutReasonChunk TimeoutError", err)
	}

	// At this point nextChunk's internal goroutine is still blocked inside
	// stream.Next() (it will stay there for up to 10s unless unblocked).
	// Close it now, exactly as processStream does immediately after a
	// timeout-induced error (stream.go's `if s := r.currentStream(); s !=
	// nil { _ = s.Close() }`), or as a consumer's own StreamTextResult.Close
	// could do concurrently at any time.
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- r.Close()
	}()

	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return promptly while a Next() call was abandoned")
	}

	// Give the abandoned Next() goroutine a moment to observe the close and
	// return, so -race has a chance to see its (now-safe) field accesses
	// land before the test process exits.
	time.Sleep(50 * time.Millisecond)
}
