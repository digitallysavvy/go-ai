package deepagents

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

type r87toolsConn struct {
	in     chan []byte
	closed chan struct{}
	once   sync.Once
}

func (c *r87toolsConn) Receive() ([]byte, error) {
	select {
	case m := <-c.in:
		return m, nil
	case <-c.closed:
		return nil, &bridge.CloseError{Code: 1000}
	}
}
func (c *r87toolsConn) Send([]byte) error { return nil }
func (c *r87toolsConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

// cr-r87-carry-to-tools 3 (class sweep from codex): a finish already buffered when a turn wires its
// listeners settles the turn during registration; the listeners registered
// after it must still be removed, so a later turn's events do not reach the
// settled turn.
func TestR87toolsSettledDuringRegistrationRemovesListeners(t *testing.T) {
	conn := &r87toolsConn{in: make(chan []byte, 16), closed: make(chan struct{})}
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
	var mu sync.Mutex
	var first []string
	pc := wireTurn(context.Background(), ch, func(p harness.StreamPart) {
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
	wireTurn(context.Background(), ch, func(p harness.StreamPart) { second <- p.PartType() })
	conn.in <- []byte(`{"type":"text-delta","seq":2,"id":"t","delta":"x"}`)
	select {
	case got := <-second:
		if got != harness.PartTypeTextDelta {
			t.Fatalf("second turn got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second turn got nothing")
	}
	conn.in <- []byte(`{"type":"error","seq":3,"error":{"message":"boom"}}`)
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("second turn got no error")
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(first) != 1 || first[0] != harness.PartTypeFinish {
		t.Fatalf("first turn got %q, want only its finish", first)
	}
}
