package bridge

import (
	"context"
	"testing"
	"time"
)

// Ports TS utils/sleep.test.ts.
func TestSleep_ReturnsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Sleep(ctx, time.Minute); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Sleep did not return after cancellation")
	}
}

func TestSleep_ReturnsImmediatelyForCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	Sleep(ctx, time.Minute)
	if time.Since(start) > time.Second {
		t.Fatal("Sleep waited on a cancelled context")
	}
}
