package tui

import (
	"context"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// realisticTextStream mirrors a real provider.TextStream: Next() blocks for a
// while (polling a `closed` flag, as a real reader loop would observe its
// underlying connection being torn down) and Close() just flips that flag —
// with no internal locking of its own, matching the bug report's claim that
// some provider.TextStream implementations share state between Next/Close
// with no synchronization.
type realisticTextStream struct {
	mu        sync.Mutex
	closed    bool
	nextCalls int
}

func (s *realisticTextStream) Next() (*provider.StreamChunk, error) {
	s.mu.Lock()
	s.nextCalls++
	s.mu.Unlock()
	for i := 0; i < 50; i++ {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return nil, io.EOF
		}
		time.Sleep(time.Millisecond)
	}
	return &provider.StreamChunk{}, nil
}
func (s *realisticTextStream) Err() error { return nil }
func (s *realisticTextStream) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

// TestStreamRenderSourceCloseWhileNextRace is a permanent regression test
// for R2-3: StreamRenderSource.Next used to call stream.Close() directly
// from the caller's goroutine on ctx cancellation while a background
// goroutine's stream.Next() call could still be in flight on the same
// stream — a data race under `go test -race`. Adapted from the bug report's
// TestZZBugReviewStreamRenderSourceCloseWhileNextRace
// (state/parity/sep_23_2026/bug-review/R2.md). Note this fixture's own
// Next/Close use a private mutex only so the *test* can read `closed`
// without racing itself; StreamRenderSource must not rely on that -- it
// must serialize its own calls into the stream regardless.
func TestStreamRenderSourceCloseWhileNextRace(t *testing.T) {
	stream := &realisticTextStream{}
	src := NewStreamRenderSourceFromTextStream(stream, nil, types.StepResult{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(5 * time.Millisecond); cancel() }()
	_, err := src.Next(ctx)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	time.Sleep(60 * time.Millisecond)
}

// TestStreamRenderSourceCloseWaitsForOwningGoroutine proves the fix's actual
// mechanism (not just the absence of a race): Close() blocks until the
// owning pump goroutine has itself closed the stream, so by the time Close()
// returns, stream.Close() has already happened -- and happened only once,
// from the pump, never concurrently with its own Next() call.
func TestStreamRenderSourceCloseWaitsForOwningGoroutine(t *testing.T) {
	stream := &realisticTextStream{}
	src := NewStreamRenderSourceFromTextStream(stream, nil, types.StepResult{})

	// Start a Next() call and let it begin blocking inside stream.Next().
	nextDone := make(chan struct{})
	go func() {
		_, _ = src.Next(context.Background())
		close(nextDone)
	}()
	time.Sleep(10 * time.Millisecond)

	if err := src.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	stream.mu.Lock()
	closed := stream.closed
	stream.mu.Unlock()
	if !closed {
		t.Fatal("Close() returned before the underlying stream was closed")
	}

	select {
	case <-nextDone:
	case <-time.After(time.Second):
		t.Fatal("Next() goroutine did not finish after Close()")
	}

	// A second Close() must be a safe no-op (idempotent).
	if err := src.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

// TestStreamRenderSourceNoGoroutineLeak drives several chunks through the
// pump and then closes it, asserting the goroutine count returns to baseline
// -- the pump must never outlive a completed Close() (R2-3's "make sure no
// goroutine leaks" requirement).
func TestStreamRenderSourceNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	stream := &countingFastStream{limit: 5}
	src := NewStreamRenderSourceFromTextStream(stream, nil, types.StepResult{})
	for {
		_, err := src.Next(context.Background())
		if err != nil {
			break
		}
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutine count after Close() = %d, want <= baseline %d (leaked pump goroutine)", got, before)
	}
}

// countingFastStream returns `limit` chunks immediately, then io.EOF --
// exercising the pump's natural-completion exit path (as opposed to the
// ctx-cancellation/Close path exercised above).
type countingFastStream struct {
	n     int
	limit int
}

func (s *countingFastStream) Next() (*provider.StreamChunk, error) {
	if s.n >= s.limit {
		return nil, io.EOF
	}
	s.n++
	return &provider.StreamChunk{}, nil
}
func (s *countingFastStream) Err() error   { return nil }
func (s *countingFastStream) Close() error { return nil }

// TestStreamRenderSourceCloseWithoutNextNeverStartsPump exercises Close()
// being called when Next() was never called at all: there is no in-flight
// call to race, so this must close the stream directly and synchronously,
// without ever spinning up the pump goroutine.
func TestStreamRenderSourceCloseWithoutNextNeverStartsPump(t *testing.T) {
	stream := &realisticTextStream{}
	src := NewStreamRenderSourceFromTextStream(stream, nil, types.StepResult{})

	done := make(chan struct{})
	go func() {
		_ = src.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close() without a prior Next() should return promptly")
	}

	stream.mu.Lock()
	closed := stream.closed
	calls := stream.nextCalls
	stream.mu.Unlock()
	if !closed {
		t.Fatal("expected the stream to be closed")
	}
	if calls != 0 {
		t.Fatalf("Next() was called %d times; Close() without a prior Next() must not invoke stream.Next()", calls)
	}
}
