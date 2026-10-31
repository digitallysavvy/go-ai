package bridge

import (
	"context"
	"testing"
)

// A turn that settled before the context was cancelled must not send an abort
// frame, even though both channels are ready and select picks either.
func TestWatchAbortSkipsSettledTurn(t *testing.T) {
	for i := 0; i < 500; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		close(done)
		cancel()
		WatchAbort(ctx, done, func() { t.Fatal("abort sent after the turn settled") })
	}
}

func TestWatchAbortSendsForLiveTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	called := make(chan struct{})
	go WatchAbort(ctx, done, func() { close(called) })
	cancel()
	<-called
}

func TestWatchAbortReturnsWhenTurnSettles(t *testing.T) {
	done := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		WatchAbort(context.Background(), done, func() { t.Error("unexpected abort") })
		close(returned)
	}()
	close(done)
	<-returned
}
