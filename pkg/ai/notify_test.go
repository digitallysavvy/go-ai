package ai

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

// CB-T05: Unit tests for Notify[E]

func TestNotify_CallsListener(t *testing.T) {
	ctx := context.Background()
	called := false
	Notify(ctx, "hello", func(_ context.Context, e string) {
		called = true
		if e != "hello" {
			t.Errorf("expected event 'hello', got %q", e)
		}
	})
	if !called {
		t.Error("listener was not called")
	}
}

func TestNotify_MultipleListeners(t *testing.T) {
	// Notify dispatches listeners concurrently (TS Promise.all semantics), so
	// only the *set* of calls is guaranteed, not their relative order.
	ctx := context.Background()
	var seen []int
	var mu sync.Mutex

	l1 := func(_ context.Context, _ int) {
		mu.Lock()
		seen = append(seen, 1)
		mu.Unlock()
	}
	l2 := func(_ context.Context, _ int) {
		mu.Lock()
		seen = append(seen, 2)
		mu.Unlock()
	}
	l3 := func(_ context.Context, _ int) {
		mu.Lock()
		seen = append(seen, 3)
		mu.Unlock()
	}

	Notify(ctx, 42, l1, l2, l3)

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(seen))
	}
	sort.Ints(seen)
	if seen[0] != 1 || seen[1] != 2 || seen[2] != 3 {
		t.Errorf("unexpected calls: %v", seen)
	}
}

// TestNotify_ListenersRunConcurrently verifies Notify does not serialize
// listeners: two blocking listeners must overlap in time.
func TestNotify_ListenersRunConcurrently(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	started := make(chan struct{}, 2)

	blocker := func(_ context.Context, _ int) {
		started <- struct{}{}
		<-release
	}

	done := make(chan struct{})
	go func() {
		Notify(ctx, 1, blocker, blocker)
		close(done)
	}()

	// Both listeners must start before either can finish, proving they run
	// concurrently rather than one-at-a-time.
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for both listeners to start concurrently")
		}
	}
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify did not return after listeners completed")
	}
}

func TestNotify_NilListenerIsSkipped(t *testing.T) {
	ctx := context.Background()
	called := false

	var nilListener Listener[string]
	realListener := func(_ context.Context, _ string) {
		called = true
	}

	// Should not panic with nil listener
	Notify(ctx, "x", nilListener, realListener)

	if !called {
		t.Error("real listener was not called after nil listener")
	}
}

func TestNotify_NoListeners(t *testing.T) {
	ctx := context.Background()
	// No panic, no-op
	Notify[string](ctx, "anything")
}

// CB-T05: Panic recovery — a panicking listener must not abort generation
func TestNotify_PanicRecovery(t *testing.T) {
	ctx := context.Background()

	secondCalled := false

	panicListener := func(_ context.Context, _ string) {
		panic("simulated listener panic")
	}
	safeListener := func(_ context.Context, _ string) {
		secondCalled = true
	}

	// Must not propagate the panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Notify propagated a panic: %v", r)
		}
	}()

	Notify(ctx, "event", panicListener, safeListener)

	if !secondCalled {
		t.Error("second listener was not called after first listener panicked")
	}
}

// CB-T05: Panic recovery — only panicking listener is skipped
func TestNotify_PanicDoesNotSkipSubsequentListeners(t *testing.T) {
	ctx := context.Background()

	var calls []int
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		n := i
		if n == 2 {
			// Listener 2 panics
			l := func(_ context.Context, _ int) { panic("boom") }
			Notify(ctx, n, l)
			continue
		}
		l := func(_ context.Context, _ int) {
			mu.Lock()
			calls = append(calls, n)
			mu.Unlock()
		}
		// Register this listener alongside a panicky one to test isolation
		_ = l
	}

	// Now do it properly with a single Notify call
	calls = nil
	l0 := func(_ context.Context, _ string) {
		mu.Lock()
		calls = append(calls, 0)
		mu.Unlock()
	}
	lPanic := func(_ context.Context, _ string) {
		panic("boom")
	}
	l2 := func(_ context.Context, _ string) {
		mu.Lock()
		calls = append(calls, 2)
		mu.Unlock()
	}
	l3 := func(_ context.Context, _ string) {
		mu.Lock()
		calls = append(calls, 3)
		mu.Unlock()
	}

	Notify(ctx, "test", l0, lPanic, l2, l3)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("expected 3 non-panicking listeners called, got %d: %v", len(calls), calls)
	}
	sort.Ints(calls)
	if calls[0] != 0 || calls[1] != 2 || calls[2] != 3 {
		t.Errorf("unexpected calls after panic: %v", calls)
	}
}
