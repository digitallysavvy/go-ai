package bridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TS "does not let a backoff delay extend the reconnect deadline": a 100 ms
// backoff with a 20 ms budget still closes with 1006 at the deadline.
func TestR89carryBackoffDoesNotExtendDeadline(t *testing.T) {
	first := newFakeConn()
	var calls atomic.Int32
	ch := NewChannel(ChannelOptions{
		Connect: func(context.Context) (Conn, error) {
			if calls.Add(1) == 1 {
				return first, nil
			}
			return nil, errors.New("connect refused")
		},
		Decode:    decodeFake,
		Reconnect: ReconnectOptions{MaxElapsed: 20 * time.Millisecond, InitialDelay: 100 * time.Millisecond, MaxDelay: 100 * time.Millisecond},
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)
	var mu sync.Mutex
	var closes []int
	ch.OnClose(func(code int, _ string) { mu.Lock(); closes = append(closes, code); mu.Unlock() })
	start := time.Now()
	first.drop()
	waitFor(t, 200*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return fmt.Sprint(closes) == "[1006]"
	})
	if time.Since(start) > 90*time.Millisecond {
		t.Fatalf("closed after %v, past the 20 ms budget plus slack", time.Since(start))
	}
	if calls.Load() < 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

// TS "closes a socket that resolves after the reconnect attempt is
// aborted".
func TestR89carryLateReconnectSocketClosed(t *testing.T) {
	first, late := newFakeConn(), newFakeConn()
	release := make(chan struct{})
	var calls atomic.Int32
	ch := NewChannel(ChannelOptions{
		Connect: func(context.Context) (Conn, error) {
			if calls.Add(1) == 1 {
				return first, nil
			}
			<-release
			return late, nil
		},
		Decode:    decodeFake,
		Reconnect: ReconnectOptions{MaxElapsed: 20 * time.Millisecond, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)
	first.drop()
	waitFor(t, 200*time.Millisecond, func() bool { return ch.IsClosed() })
	close(release)
	waitFor(t, time.Second, func() bool {
		select {
		case <-late.closed:
			return true
		default:
			return false
		}
	})
}
