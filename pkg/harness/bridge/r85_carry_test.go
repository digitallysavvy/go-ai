package bridge

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TS 2c12e256d2 (sandbox-channel.test.ts): sends are gated on the socket
// being OPEN, and a flush keeps the frames it could not send.

// r85carryConn is a fakeConn with a TS-like readyState: setClosing makes it
// report "not open" before its close event (drop) arrives.
type r85carryConn struct {
	*fakeConn
	notOpen atomic.Bool
}

func newR85carryConn() *r85carryConn { return &r85carryConn{fakeConn: newFakeConn()} }

func (c *r85carryConn) isOpen() bool { return !c.notOpen.Load() }
func (c *r85carryConn) markClosed()  { c.notOpen.Store(true) }
func (c *r85carryConn) Close() error {
	c.notOpen.Store(true)
	return c.fakeConn.Close()
}
func (c *r85carryConn) drop() {
	c.notOpen.Store(true)
	c.fakeConn.drop()
}

// r85carrySockets hands out the given sockets in order.
type r85carrySockets struct {
	mu    sync.Mutex
	conns []*r85carryConn
	n     int
}

func (s *r85carrySockets) connect(context.Context) (Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n >= len(s.conns) {
		return nil, errors.New("no more sockets")
	}
	c := s.conns[s.n]
	s.n++
	return c, nil
}

func (s *r85carrySockets) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func r85carryChannel(t *testing.T, s *r85carrySockets) *Channel {
	t.Helper()
	ch := NewChannel(ChannelOptions{Connect: s.connect, Decode: decodeFake, Reconnect: testReconnect()})
	t.Cleanup(ch.Close)
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return ch
}

const (
	r85carryResume = `{"type":"resume","lastSeenEventId":0}`
	r85carryAbort  = `{"type":"abort"}`
	r85carryStop   = `{"type":"stop"}`
)

func r85carryWaitSent(t *testing.T, c *r85carryConn, want []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !reflect.DeepEqual(c.Sent(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("sent = %q, want %q", c.Sent(), want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TS: "queues sends before the close event at readyState 2" / "... 3".
func TestR85carryQueuesSendsBeforeCloseEvent(t *testing.T) {
	first, second := newR85carryConn(), newR85carryConn()
	s := &r85carrySockets{conns: []*r85carryConn{first, second}}
	ch := r85carryChannel(t, s)
	first.notOpen.Store(true) // CLOSING/CLOSED, close event not delivered yet

	if err := ch.Send(AbortCommand{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := first.Sent(); len(got) != 0 {
		t.Fatalf("first socket got %q while not open, want nothing", got)
	}
	first.drop()
	r85carryWaitSent(t, second, []string{r85carryResume, r85carryAbort})
}

// TS: "retains unsent frames when a reconnecting socket is closing during the
// flush".
func TestR85carryFlushRetainsFramesWhenSocketCloses(t *testing.T) {
	first, second, third := newR85carryConn(), newR85carryConn(), newR85carryConn()
	second.onSend = func(data []byte) error {
		if string(data) == r85carryStop {
			// The frame is written, then the socket starts closing.
			second.notOpen.Store(true)
		}
		return nil
	}
	s := &r85carrySockets{conns: []*r85carryConn{first, second, third}}
	ch := r85carryChannel(t, s)
	first.drop()
	for _, cmd := range []InboundCommand{StopCommand{}, AbortCommand{}, StopCommand{}} {
		if err := ch.Send(cmd); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	r85carryWaitSent(t, second, []string{r85carryResume, r85carryStop})
	second.drop()
	r85carryWaitSent(t, third, []string{r85carryResume, r85carryAbort, r85carryStop})
	if s.count() != 3 {
		t.Fatalf("connections = %d, want 3", s.count())
	}
}

// TS: "retains unsent frames when a reconnecting socket is throwing during the
// flush". A write that fails keeps that frame and the rest queued, and the
// channel connects again.
func TestR85carryFlushRetainsFramesWhenWriteFails(t *testing.T) {
	first, second, third := newR85carryConn(), newR85carryConn(), newR85carryConn()
	second.onSend = func(data []byte) error {
		if string(data) == r85carryAbort {
			return errors.New("send failed")
		}
		return nil
	}
	s := &r85carrySockets{conns: []*r85carryConn{first, second, third}}
	ch := r85carryChannel(t, s)
	first.drop()
	for _, cmd := range []InboundCommand{StopCommand{}, AbortCommand{}, StopCommand{}} {
		if err := ch.Send(cmd); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	r85carryWaitSent(t, third, []string{r85carryResume, r85carryAbort, r85carryStop})
	if got := second.Sent(); !reflect.DeepEqual(got, []string{r85carryResume, r85carryStop}) {
		t.Fatalf("second sent = %q, want resume, stop", got)
	}
	if s.count() != 3 {
		t.Fatalf("connections = %d, want 3", s.count())
	}
}

// Go: a frame whose write fails on a connected socket is kept and sent again
// after the reconnect (the failed socket is closed, as `ws` destroys a socket
// whose write failed), instead of being dropped.
func TestR85carryFailedDirectSendIsResentAfterReconnect(t *testing.T) {
	first, second := newR85carryConn(), newR85carryConn()
	first.onSend = func([]byte) error { return errors.New("broken pipe") }
	s := &r85carrySockets{conns: []*r85carryConn{first, second}}
	ch := r85carryChannel(t, s)
	if err := ch.Send(AbortCommand{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	r85carryWaitSent(t, second, []string{r85carryResume, r85carryAbort})
}

// wsConn reports itself not open once closed or once a read failed.
func TestR85carryWSConnOpenState(t *testing.T) {
	var c wsConn
	if !c.isOpen() {
		t.Fatal("new wsConn not open")
	}
	c.markClosed()
	if c.isOpen() {
		t.Fatal("wsConn open after markClosed")
	}
}
