package bridge

import (
	"context"
	"testing"
	"time"
)

// Port of harness/src/utils/sleep.test.ts.

// TS: "resolves when the abort signal fires"
func TestSleepResolvesWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Sleep(ctx, time.Second)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Sleep did not return promptly after cancellation")
	}
}

// TS: "resolves immediately for an already-aborted signal"
func TestSleepResolvesImmediatelyForCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		Sleep(ctx, time.Second)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Sleep did not return promptly for an already-canceled context")
	}
}
