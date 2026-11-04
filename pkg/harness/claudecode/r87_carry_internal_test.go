package claudecode

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// TS 3bf70f83bc: "cleans up a turn whose initial start cannot be sent". The
// send error settles the turn, stops the deadline and removes the listeners.
func TestR87carryInitialStartSendErrorCleansUp(t *testing.T) {
	ch := bridge.NewChannel(bridge.ChannelOptions{})
	ch.Close()
	deadline := time.Now().Add(2 * time.Second)
	for !ch.IsClosed() {
		if time.Now().After(deadline) {
			t.Fatal("channel did not close")
		}
		time.Sleep(time.Millisecond)
	}
	s := newSession(sessionOptions{sessionID: "s1", channel: ch, turnStartTimeout: 50 * time.Millisecond})
	pc := s.wireTurn(context.Background(), func(harness.StreamPart) {})
	err := pc.sendStart(&StartFrame{})
	if err == nil || err.Error() != "SandboxChannel: cannot send start — channel is closed." {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-pc.Done():
	default:
		t.Fatal("the turn did not settle")
	}
	pc.mu.Lock()
	timer, pending := pc.startTimer, pc.pendingStart
	pc.mu.Unlock()
	if timer != nil || pending != nil {
		t.Fatalf("timer = %v, pending = %v", timer, pending)
	}
	// The listeners are removed: a later Add unsubscribes at once.
	removed := false
	pc.subs.Add(func() { removed = true })
	if !removed {
		t.Fatal("the turn's subscriptions were not released")
	}
	if pc.Err() != err {
		t.Fatalf("Err = %v, want the send error", pc.Err())
	}
}

// Ruling 5: zero is the TS default (120 s), a negative value disables it.
func TestR87carryTurnStartTimeoutDefault(t *testing.T) {
	for in, want := range map[time.Duration]time.Duration{0: 120 * time.Second, -time.Second: 0, 5 * time.Second: 5 * time.Second} {
		if got := turnStartTimeout(Settings{TurnStartTimeout: in}); got != want {
			t.Errorf("turnStartTimeout(%v) = %v, want %v", in, got, want)
		}
	}
}

// r87carryConn is an in-memory bridge socket for internal tests.
type r87carryConn struct {
	in     chan []byte
	closed chan struct{}
	once   sync.Once
}

func newR87carryConn() *r87carryConn {
	return &r87carryConn{in: make(chan []byte, 16), closed: make(chan struct{})}
}

func (c *r87carryConn) Receive() ([]byte, error) {
	select {
	case m := <-c.in:
		return m, nil
	case <-c.closed:
		return nil, &bridge.CloseError{Code: 1000}
	}
}
func (c *r87carryConn) Send([]byte) error { return nil }
func (c *r87carryConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

// A finish already buffered when a turn wires its listeners settles the turn
// during registration; the listeners registered after it must still be
// removed, so a later turn's events do not reach the settled turn.
func TestR87carrySettledDuringRegistrationRemovesListeners(t *testing.T) {
	conn := newR87carryConn()
	ch := bridge.NewChannel(bridge.ChannelOptions{Connect: func(context.Context) (bridge.Conn, error) { return conn, nil }})
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ch.Close)
	conn.in <- []byte(`{"type":"finish","seq":1,"finishReason":{"unified":"stop","raw":"stop"},"totalUsage":{"inputTokens":{},"outputTokens":{}}}`)
	deadline := time.Now().Add(2 * time.Second)
	for ch.LastSeenEventID() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("finish not received")
		}
		time.Sleep(time.Millisecond)
	}
	s := newSession(sessionOptions{sessionID: "s1", channel: ch})
	var mu sync.Mutex
	var first []string
	pc := s.wireTurn(context.Background(), func(p harness.StreamPart) {
		mu.Lock()
		first = append(first, p.PartType())
		mu.Unlock()
	})
	select {
	case <-pc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the buffered finish did not settle the turn")
	}
	second := make(chan string, 4)
	s.wireTurn(context.Background(), func(p harness.StreamPart) { second <- p.PartType() })
	conn.in <- []byte(`{"type":"text-delta","seq":2,"id":"t","delta":"x"}`)
	select {
	case got := <-second:
		if got != harness.PartTypeTextDelta {
			t.Fatalf("second turn got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second turn got nothing")
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(first) != 1 || first[0] != harness.PartTypeFinish {
		t.Fatalf("first turn got %q, want only its finish", first)
	}
}
