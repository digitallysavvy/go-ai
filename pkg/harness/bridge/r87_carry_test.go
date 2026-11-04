package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// TS 3bf70f83bc (sandbox-channel.test.ts, harness-v1-bridge-protocol.test.ts):
// send options `onSent` / `queueIfDisconnected`, `startId` and the
// `idempotentStart` hello capability.

// r87carryGatedSockets hands out the given sockets in order; the second and
// later connects wait until release is closed, so a test can act while the
// channel is disconnected.
type r87carryGatedSockets struct {
	r85carrySockets
	release chan struct{}
}

func (s *r87carryGatedSockets) connect(ctx context.Context) (Conn, error) {
	if s.count() > 0 {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.r85carrySockets.connect(ctx)
}

func r87carryGatedChannel(t *testing.T, conns ...*r85carryConn) (*Channel, *r87carryGatedSockets) {
	t.Helper()
	s := &r87carryGatedSockets{r85carrySockets: r85carrySockets{conns: conns}, release: make(chan struct{})}
	ch := NewChannel(ChannelOptions{Connect: s.connect, Decode: decodeFake, Reconnect: ReconnectOptions{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxElapsed: 2 * time.Second}})
	t.Cleanup(ch.Close)
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return ch, s
}

type r87carryLog struct {
	mu  sync.Mutex
	got []string
}

func (l *r87carryLog) add(s string) func() {
	return func() {
		l.mu.Lock()
		l.got = append(l.got, s)
		l.mu.Unlock()
	}
}

func (l *r87carryLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.got...)
}

func (l *r87carryLog) waitFor(t *testing.T, want []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !reflect.DeepEqual(l.list(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("notifications = %q, want %q", l.list(), want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TS: "notifies sends asynchronously and after reconnect handlers for queued
// frames".
func TestR87carryOnSentAfterReconnectHandlersForQueuedFrames(t *testing.T) {
	first, second := newR85carryConn(), newR85carryConn()
	ch, s := r87carryGatedChannel(t, first, second)
	var log r87carryLog
	if err := ch.SendWithOptions(StartBase{Prompt: "hi"}, SendOptions{OnSent: log.add("start")}); err != nil {
		t.Fatal(err)
	}
	log.waitFor(t, []string{"start"})

	first.drop()
	waitFor(t, time.Second, func() bool {
		ch.mu.Lock()
		defer ch.mu.Unlock()
		return !ch.connected
	})
	if err := ch.SendWithOptions(AbortCommand{}, SendOptions{OnSent: log.add("abort")}); err != nil {
		t.Fatal(err)
	}
	ch.OnReconnect(log.add("reconnected"))
	time.Sleep(20 * time.Millisecond)
	if got := log.list(); !reflect.DeepEqual(got, []string{"start"}) {
		t.Fatalf("a queued frame notified before it was sent: %q", got)
	}
	close(s.release)
	log.waitFor(t, []string{"start", "reconnected", "abort"})
	r85carryWaitSent(t, second, []string{r85carryResume, r85carryAbort})
}

// TS: "lets the caller own replay when the socket is disconnected" / "... 2"
// / "... 3". A frame that cannot be written now is discarded and never
// notified; the next connection gets only the resume.
func TestR87carryCallerOwnsReplay(t *testing.T) {
	noQueue := false
	for _, state := range []string{"disconnected", "not open", "write fails"} {
		t.Run(state, func(t *testing.T) {
			first, second := newR85carryConn(), newR85carryConn()
			ch, s := r87carryGatedChannel(t, first, second)
			switch state {
			case "disconnected":
				first.drop()
				waitFor(t, time.Second, func() bool {
					ch.mu.Lock()
					defer ch.mu.Unlock()
					return !ch.connected
				})
			case "not open":
				first.notOpen.Store(true)
			case "write fails":
				first.onSend = func([]byte) error { return errors.New("broken pipe") }
			}
			var log r87carryLog
			if err := ch.SendWithOptions(AbortCommand{}, SendOptions{OnSent: log.add("abort"), QueueIfDisconnected: &noQueue}); err != nil {
				t.Fatal(err)
			}
			if state == "not open" {
				first.drop()
			}
			close(s.release)
			r85carryWaitSent(t, second, []string{r85carryResume})
			time.Sleep(20 * time.Millisecond)
			if got := first.Sent(); len(got) != 0 {
				t.Fatalf("first socket got %q", got)
			}
			if got := second.Sent(); !reflect.DeepEqual(got, []string{r85carryResume}) {
				t.Fatalf("second socket got %q, want only the resume", got)
			}
			if got := log.list(); len(got) != 0 {
				t.Fatalf("a discarded frame was notified: %q", got)
			}
		})
	}
}

// TS: "queues sends before the close event at readyState 2/3" (onSent
// once, after the flush) and "retains unsent frames when a reconnecting socket
// is closing/throwing during the flush" (each onSent exactly once).
func TestR87carryQueuedFramesNotifiedOnce(t *testing.T) {
	t.Run("queued before close event", func(t *testing.T) {
		first, second := newR85carryConn(), newR85carryConn()
		s := &r85carrySockets{conns: []*r85carryConn{first, second}}
		ch := r85carryChannel(t, s)
		first.notOpen.Store(true)
		var log r87carryLog
		if err := ch.SendWithOptions(AbortCommand{}, SendOptions{OnSent: log.add("abort")}); err != nil {
			t.Fatal(err)
		}
		if got := log.list(); len(got) != 0 {
			t.Fatalf("notified before the write: %q", got)
		}
		first.drop()
		r85carryWaitSent(t, second, []string{r85carryResume, r85carryAbort})
		log.waitFor(t, []string{"abort"})
	})
	for _, mode := range []string{"closing", "throwing"} {
		t.Run(mode, func(t *testing.T) {
			first, second, third := newR85carryConn(), newR85carryConn(), newR85carryConn()
			second.onSend = func(data []byte) error {
				if mode == "throwing" && string(data) == r85carryAbort {
					return errors.New("send failed")
				}
				if mode == "closing" && string(data) == r85carryStop {
					second.notOpen.Store(true)
				}
				return nil
			}
			s := &r85carrySockets{conns: []*r85carryConn{first, second, third}}
			ch := r85carryChannel(t, s)
			first.drop()
			var log r87carryLog
			for i, cmd := range []InboundCommand{StopCommand{}, AbortCommand{}, StopCommand{}} {
				if err := ch.SendWithOptions(cmd, SendOptions{OnSent: log.add([]string{"first", "abort", "last"}[i])}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "closing" {
				r85carryWaitSent(t, second, []string{r85carryResume, r85carryStop})
				second.drop()
			}
			r85carryWaitSent(t, third, []string{r85carryResume, r85carryAbort, r85carryStop})
			log.waitFor(t, []string{"first", "abort", "last"})
		})
	}
}

// A send on a closed channel fails and never notifies.
func TestR87carryClosedChannelSendNotNotified(t *testing.T) {
	first := newR85carryConn()
	ch, _ := r87carryGatedChannel(t, first)
	ch.Close()
	waitFor(t, time.Second, ch.IsClosed)
	called := false
	if err := ch.SendWithOptions(AbortCommand{}, SendOptions{OnSent: func() { called = true }}); err == nil {
		t.Fatal("want an error on a closed channel")
	}
	if called {
		t.Fatal("onSent ran for a frame that was not sent")
	}
}

// TS harness-v1-bridge-protocol.test.ts: "preserves start identifiers and the
// retry capability"; `idempotentStart` is nullish.
func TestR87carryStartIDAndIdempotentStartCapability(t *testing.T) {
	data, err := MarshalInbound(StartBase{Prompt: "Hello", StartID: "turn-1"})
	if err != nil {
		t.Fatal(err)
	}
	var start map[string]any
	if err := json.Unmarshal(data, &start); err != nil {
		t.Fatal(err)
	}
	if start["startId"] != "turn-1" || start["type"] != "start" {
		t.Fatalf("start = %s", data)
	}
	data, _ = MarshalInbound(StartBase{Prompt: "Hello"})
	start = nil
	if err := json.Unmarshal(data, &start); err != nil {
		t.Fatal(err)
	}
	if _, ok := start["startId"]; ok {
		t.Fatalf("an unset startId was sent: %s", data)
	}

	for raw, want := range map[string]bool{
		`{"type":"bridge-hello","capabilities":{"idempotentStart":true}}`:  true,
		`{"type":"bridge-hello","capabilities":{"idempotentStart":null}}`:  false,
		`{"type":"bridge-hello","capabilities":{"idempotentStart":false}}`: false,
		`{"type":"bridge-hello"}`: false,
	} {
		hello, ok := parseHello([]byte(raw))
		if !ok {
			t.Fatalf("%s did not parse", raw)
		}
		if got := hello.SupportsIdempotentStart(); got != want {
			t.Errorf("%s: SupportsIdempotentStart = %v, want %v", raw, got, want)
		}
	}
	if _, ok := parseHello([]byte(`{"type":"bridge-hello","capabilities":{"idempotentStart":"yes"}}`)); ok {
		t.Error("a non-boolean idempotentStart parsed")
	}
	out, err := MarshalOutbound(&Hello{State: "waiting", Capabilities: &HelloCapabilities{IdempotentStart: new(bool)}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"type":"bridge-hello","state":"waiting","capabilities":{"idempotentStart":false}}` {
		t.Errorf("hello = %s", out)
	}
}
