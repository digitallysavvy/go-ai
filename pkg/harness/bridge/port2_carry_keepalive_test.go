package bridge

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TS sandbox-channel-keepalive.test.ts 'closes after the reconnect budget
// expires for a dead peer': the first socket never answers, the replacement
// connections fail, so the channel gives up with 1006 'reconnect failed',
// reports reconnect-failed last, and leaves no heartbeat running.
func TestPort2carryKeepAliveDeadPeerClosesAfterReconnectBudget(t *testing.T) {
	var attempts atomic.Int32
	first := &portcarryharHBConn{fakeConn: newFakeConn()}
	var mu sync.Mutex
	var events []string
	ch := NewChannel(ChannelOptions{
		Connect: func(context.Context) (Conn, error) {
			if attempts.Add(1) == 1 {
				return first, nil
			}
			return nil, errors.New("peer unavailable")
		},
		Decode:    decodeFake,
		KeepAlive: portcarryharTick,
		Reconnect: ReconnectOptions{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxElapsed: 100 * time.Millisecond},
		OnDebug: func(e ChannelDebugEvent) {
			mu.Lock()
			events = append(events, e.Event)
			mu.Unlock()
		},
	})
	t.Cleanup(ch.Close)
	type closed struct {
		code   int
		reason string
	}
	closes := make(chan closed, 4)
	ch.OnClose(func(code int, reason string) { closes <- closed{code, reason} })
	mustOpen(t, ch)

	select {
	case c := <-closes:
		if c.code != 1006 || c.reason != "reconnect failed" {
			t.Fatalf("close = %+v, want 1006 'reconnect failed'", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the channel did not close")
	}
	if !ch.IsClosed() {
		t.Fatal("the channel is not closed")
	}
	if !first.isClosed() {
		t.Fatal("the silent socket was not terminated")
	}
	if got := first.pings.Load(); got != 2 {
		t.Fatalf("pings on the dead socket = %d, want 2", got)
	}
	time.Sleep(3 * portcarryharTick)
	if got := first.pings.Load(); got != 2 {
		t.Fatalf("pings after the channel closed = %d, want 2 (heartbeat still running)", got)
	}
	select {
	case c := <-closes:
		t.Fatalf("second close notification %+v, want exactly one", c)
	default:
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 || events[len(events)-1] != DebugReconnectFailed {
		t.Fatalf("debug events = %v, want reconnect-failed last", events)
	}
}
