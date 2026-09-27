package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Port of harness/src/utils/sandbox-channel.test.ts. fakeConn plays the role
// of the TS test's fake `ws` socket, and connector the role of `makeConnector`
// (a Conn.Send-recording, per-call fresh socket).

// fakeFrame is a minimal OutboundMessage for tests, mirroring the TS test's
// custom outboundSchema (text-delta / finish / finish-step / compaction).
type fakeFrame struct {
	typ   string
	delta string
}

func (f fakeFrame) FrameType() string { return f.typ }

// decodeFake mirrors the TS test's zod discriminated union: text-delta,
// finish, finish-step, compaction, sandbox-log, debug-event, error, plus
// "mystery" as an intentionally-unknown type (malformed-message test).
func decodeFake(data []byte) (OutboundMessage, *float64, error) {
	var probe struct {
		Type  string          `json:"type"`
		Delta string          `json:"delta"`
		Seq   *float64        `json:"seq"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, nil, err
	}
	switch probe.Type {
	case "text-delta", "finish", "finish-step", "compaction", "sandbox-log", "debug-event":
		return fakeFrame{typ: probe.Type, delta: probe.Delta}, probe.Seq, nil
	case "error":
		var errVal any
		if len(probe.Error) > 0 {
			_ = json.Unmarshal(probe.Error, &errVal)
		}
		return StreamPartFrame{Part: &harness.ErrorPart{Error: errVal}}, probe.Seq, nil
	default:
		return nil, probe.Seq, fmt.Errorf("unknown frame type %q", probe.Type)
	}
}

// fakeConn is an in-memory Conn: deliver() simulates an inbound frame (like
// the TS fake socket's `deliver`), drop() simulates an abrupt disconnect, and
// Sent() exposes everything written to it (like the TS fake socket's `sent`).
type fakeConn struct {
	msgCh  chan []byte
	closed chan struct{}

	mu       sync.Mutex
	closeErr *CloseError

	sentMu sync.Mutex
	sent   []string

	onSend func(data []byte) error
}

func newFakeConn() *fakeConn {
	return &fakeConn{msgCh: make(chan []byte, 256), closed: make(chan struct{})}
}

func (c *fakeConn) Receive() ([]byte, error) {
	// Prefer already-queued messages over a concurrent drop so buffered
	// frames are not lost to a race with drop().
	select {
	case m := <-c.msgCh:
		return m, nil
	default:
	}
	select {
	case m := <-c.msgCh:
		return m, nil
	case <-c.closed:
		c.mu.Lock()
		ce := c.closeErr
		c.mu.Unlock()
		if ce != nil {
			return nil, ce
		}
		return nil, &CloseError{Code: 1000}
	}
}

func (c *fakeConn) Send(data []byte) error {
	if c.onSend != nil {
		if err := c.onSend(data); err != nil {
			return err
		}
	}
	c.sentMu.Lock()
	c.sent = append(c.sent, string(data))
	c.sentMu.Unlock()
	return nil
}

func (c *fakeConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

// deliverRaw simulates one inbound frame from already-encoded JSON bytes
// (e.g. a recorded fixture line), bypassing deliver's map-based construction.
func (c *fakeConn) deliverRaw(data []byte) {
	select {
	case c.msgCh <- data:
	case <-c.closed:
	}
}

// deliver simulates one inbound frame, optionally stamping seq.
func (c *fakeConn) deliver(obj map[string]any, seq ...float64) {
	if len(seq) > 0 {
		obj["seq"] = seq[0]
	}
	data, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	select {
	case c.msgCh <- data:
	case <-c.closed:
	}
}

// drop simulates an abrupt disconnect (default code 1006, "socket error").
func (c *fakeConn) drop(code ...int) {
	co := 1006
	if len(code) > 0 {
		co = code[0]
	}
	c.mu.Lock()
	c.closeErr = &CloseError{Code: co, Reason: "socket error"}
	c.mu.Unlock()
	_ = c.Close()
}

func (c *fakeConn) Sent() []string {
	c.sentMu.Lock()
	defer c.sentMu.Unlock()
	return append([]string(nil), c.sent...)
}

// connector hands out a fresh fakeConn per Connect call and records them,
// mirroring the TS test's makeConnector.
type connector struct {
	mu    sync.Mutex
	conns []*fakeConn
}

func newConnector() *connector { return &connector{} }

func (c *connector) connect(context.Context) (Conn, error) {
	fc := newFakeConn()
	c.mu.Lock()
	c.conns = append(c.conns, fc)
	c.mu.Unlock()
	return fc, nil
}

func (c *connector) current() *fakeConn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conns[len(c.conns)-1]
}

func (c *connector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.conns)
}

// flush gives goroutines and the SelectiveFlushGrace timer time to run.
func flush() { time.Sleep(50 * time.Millisecond) }

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func testReconnect() ReconnectOptions {
	return ReconnectOptions{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxElapsed: 200 * time.Millisecond}
}

func newTestChannel(t *testing.T, c *connector, onDebug func(ChannelDebugEvent)) *Channel {
	t.Helper()
	ch := NewChannel(ChannelOptions{
		Connect:   c.connect,
		Decode:    decodeFake,
		Reconnect: testReconnect(),
		OnDebug:   onDebug,
	})
	t.Cleanup(ch.Close)
	return ch
}

func mustOpen(t *testing.T, ch *Channel) {
	t.Helper()
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatalf("Open: %v", err)
	}
}

// TS: "dispatches outbound messages by type"
func TestChannelDispatchesByType(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	var mu sync.Mutex
	var text []string
	ch.On("text-delta", func(e Event) {
		mu.Lock()
		text = append(text, e.Message.(fakeFrame).delta)
		mu.Unlock()
	})
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "hello"})
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": " world"})
	// Wait for both deltas rather than a fixed sleep: under -race and
	// parallel package load, 50ms is not always enough for dispatch.
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(text) >= 2
	})

	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(text) != fmt.Sprint([]string{"hello", " world"}) {
		t.Fatalf("text = %v", text)
	}
}

// TS: "replays messages buffered before the listener subscribes"
func TestChannelReplaysBufferedBeforeSubscribe(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	c.current().deliver(map[string]any{"type": "finish"})
	flush()

	var captured int
	ch.On("finish", func(Event) { captured++ })
	if captured != 1 {
		t.Fatalf("captured = %d, want 1", captured)
	}
}

// TS: "replays buffered messages in arrival order across event types"
func TestChannelReplaysBufferedOrderAcrossTypes(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	c.current().deliver(map[string]any{"type": "finish-step"})
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "result"})
	c.current().deliver(map[string]any{"type": "finish-step"})
	c.current().deliver(map[string]any{"type": "finish"})
	flush()

	var captured []string
	ch.On("text-delta", func(e Event) { captured = append(captured, e.Type()) })
	if len(captured) != 0 {
		t.Fatalf("captured after text-delta subscribe = %v, want []", captured)
	}

	ch.On("finish-step", func(e Event) { captured = append(captured, e.Type()) })
	want := []string{"finish-step", "text-delta", "finish-step"}
	if fmt.Sprint(captured) != fmt.Sprint(want) {
		t.Fatalf("captured = %v, want %v", captured, want)
	}

	ch.On("finish", func(e Event) { captured = append(captured, e.Type()) })
	want = []string{"finish-step", "text-delta", "finish-step", "finish"}
	if fmt.Sprint(captured) != fmt.Sprint(want) {
		t.Fatalf("captured = %v, want %v", captured, want)
	}
}

// TS: "preserves arrival order across an explicit asynchronous listener attachment"
func TestChannelPreservesOrderAcrossExplicitAttachment(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	finish := ch.BeginListenerAttachment()
	mustOpen(t, ch)
	c.current().deliver(map[string]any{"type": "finish-step"})
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "result"})
	flush()

	var captured []string
	ch.On("text-delta", func(e Event) { captured = append(captured, e.Type()) })
	time.Sleep(10 * time.Millisecond)
	ch.On("finish-step", func(e Event) { captured = append(captured, e.Type()) })
	if len(captured) != 0 {
		t.Fatalf("captured before finishing attachment = %v, want []", captured)
	}

	finish()
	want := []string{"finish-step", "text-delta"}
	if fmt.Sprint(captured) != fmt.Sprint(want) {
		t.Fatalf("captured = %v, want %v", captured, want)
	}
}

// TS: "holds events that arrive while listeners are attaching"
func TestChannelHoldsEventsWhileAttaching(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	finish := ch.BeginListenerAttachment()
	mustOpen(t, ch)

	var captured []string
	ch.On("text-delta", func(e Event) { captured = append(captured, e.Message.(fakeFrame).delta) })
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "result"})
	flush()
	if len(captured) != 0 {
		t.Fatalf("captured before finishing attachment = %v, want []", captured)
	}

	finish()
	if fmt.Sprint(captured) != fmt.Sprint([]string{"result"}) {
		t.Fatalf("captured = %v", captured)
	}
}

// TS: "does not block subscribed events behind an unhandled buffered type"
func TestChannelDoesNotBlockSubscribedBehindUnhandledType(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	c.current().deliver(map[string]any{"type": "compaction"})
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "result"})
	c.current().deliver(map[string]any{"type": "finish"})
	flush()

	var mu sync.Mutex
	var captured []string
	ch.On("text-delta", func(e Event) {
		mu.Lock()
		captured = append(captured, e.Type())
		mu.Unlock()
	})
	ch.On("finish", func(e Event) {
		mu.Lock()
		captured = append(captured, e.Type())
		mu.Unlock()
	})
	// The selective flush that delivers these runs on a background timer
	// goroutine (SelectiveFlushGrace), not synchronously within On(); wait for
	// it instead of racing a bare sleep against the mutex-guarded slice.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(captured) == 2
	})

	mu.Lock()
	want := []string{"text-delta", "finish"}
	if fmt.Sprint(captured) != fmt.Sprint(want) {
		t.Fatalf("captured = %v, want %v", captured, want)
	}
	mu.Unlock()

	var compactionsMu sync.Mutex
	var compactions []Event
	ch.On("compaction", func(e Event) {
		compactionsMu.Lock()
		compactions = append(compactions, e)
		compactionsMu.Unlock()
	})
	compactionsMu.Lock()
	defer compactionsMu.Unlock()
	if len(compactions) != 1 {
		t.Fatalf("compactions = %d, want 1", len(compactions))
	}
}

// TS: "delivers an already buffered subscribed event after immediate unsubscribe"
func TestChannelDeliversBufferedEventAfterImmediateUnsubscribe(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	c.current().deliver(map[string]any{"type": "compaction"})
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "result"})
	flush()

	var mu sync.Mutex
	var captured []string
	unsubscribe := ch.On("text-delta", func(e Event) {
		mu.Lock()
		captured = append(captured, e.Message.(fakeFrame).delta)
		mu.Unlock()
	})
	unsubscribe()
	// The pinned buffered event is delivered by the selective-flush grace
	// timer (background goroutine), not synchronously here.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(captured) == 1
	})

	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(captured) != fmt.Sprint([]string{"result"}) {
		t.Fatalf("captured = %v", captured)
	}
}

// TS: 'suspend freezes the cursor at the last delivered event and closes with reason "suspended"'
func TestChannelSuspendFreezesCursor(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	var mu sync.Mutex
	var text []string
	ch.On("text-delta", func(e Event) {
		mu.Lock()
		text = append(text, e.Message.(fakeFrame).delta)
		mu.Unlock()
	})
	var closeReason string
	ch.OnClose(func(_ int, reason string) {
		mu.Lock()
		closeReason = reason
		mu.Unlock()
	})

	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "one"}, 1)
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "two"}, 2)

	// Unlike TS (single-threaded: the fake socket's synchronous message
	// handler enqueues both frames before suspend() ever runs), delivery here
	// happens on a separate reader goroutine, so wait for both frames to be
	// dispatched before suspending — otherwise Suspend could freeze the
	// cursor before the reader goroutine has even seen them.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(text) == 2
	})

	cursor := <-ch.Suspend()
	mu.Lock()
	if cursor != 2 {
		t.Fatalf("cursor = %v, want 2", cursor)
	}
	if closeReason != CloseReasonSuspended {
		t.Fatalf("closeReason = %q, want %q", closeReason, CloseReasonSuspended)
	}
	mu.Unlock()
	if ch.LastSeenEventID() != 2 {
		t.Fatalf("LastSeenEventID = %v, want 2", ch.LastSeenEventID())
	}

	// Frames arriving after suspend are ignored.
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "three"}, 3)
	flush()
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(text) != fmt.Sprint([]string{"one", "two"}) {
		t.Fatalf("text = %v", text)
	}
	if ch.LastSeenEventID() != 2 {
		t.Fatalf("LastSeenEventID after post-suspend frame = %v, want 2", ch.LastSeenEventID())
	}

	// No resume was sent — suspend is a one-way close, not a reconnect.
	if got := c.current().Sent(); len(got) != 0 {
		t.Fatalf("sent = %v, want none", got)
	}
}

// TS: "serialises and sends inbound messages"
func TestChannelSerializesAndSendsInbound(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	if err := ch.Send(AbortCommand{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	want := []string{`{"type":"abort"}`}
	if fmt.Sprint(c.current().Sent()) != fmt.Sprint(want) {
		t.Fatalf("sent = %v, want %v", c.current().Sent(), want)
	}
}

// TS: "suspends from a pinned event even after later events were dispatched"
func TestChannelSuspendsFromPinnedCheckpoint(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	var mu sync.Mutex
	var finishStep Event
	var haveFinishStep bool
	ch.On("finish-step", func(e Event) {
		mu.Lock()
		finishStep, haveFinishStep = e, true
		mu.Unlock()
	})
	ch.On("text-delta", func(Event) {})

	c.current().deliver(map[string]any{"type": "finish-step"}, 1)
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "next"}, 2)
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": " step"}, 3)
	waitFor(t, time.Second, func() bool { return ch.LastSeenEventID() == 3 })
	mu.Lock()
	ok := haveFinishStep
	fs := finishStep
	mu.Unlock()
	if !ok {
		t.Fatal("finish-step listener never fired")
	}
	release := fs.PinCheckpoint()
	if release == nil {
		t.Fatal("PinCheckpoint returned nil for an event with a seq")
	}
	if got := c.current().Sent(); len(got) != 0 {
		t.Fatalf("sent = %v, want none", got)
	}

	cursor := <-ch.Suspend()
	if cursor != 1 {
		t.Fatalf("cursor = %v, want 1 (pinned)", cursor)
	}
}

// TS: "returns to the latest cursor after releasing a checkpoint"
func TestChannelReturnsToLatestCursorAfterRelease(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	var mu sync.Mutex
	var finishStep Event
	ch.On("finish-step", func(e Event) {
		mu.Lock()
		finishStep = e
		mu.Unlock()
	})
	ch.On("text-delta", func(Event) {})

	c.current().deliver(map[string]any{"type": "finish-step"}, 1)
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "next"}, 2)
	waitFor(t, time.Second, func() bool { return ch.LastSeenEventID() == 2 })

	mu.Lock()
	fs := finishStep
	mu.Unlock()
	release := fs.PinCheckpoint()
	if release == nil {
		t.Fatal("PinCheckpoint returned nil")
	}
	release()

	cursor := <-ch.Suspend()
	if cursor != 2 {
		t.Fatalf("cursor = %v, want 2", cursor)
	}
}

// TS: "refuses to send once terminally closed"
func TestChannelRefusesSendOnceClosed(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)
	ch.Close()
	flush()

	err := ch.Send(AbortCommand{})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Send after Close: err = %v, want an error mentioning \"closed\"", err)
	}
}

// TS: "surfaces malformed messages as error events"
func TestChannelSurfacesMalformedMessagesAsErrors(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	var mu sync.Mutex
	var errs []Event
	ch.On("error", func(e Event) {
		mu.Lock()
		errs = append(errs, e)
		mu.Unlock()
	})
	c.current().deliver(map[string]any{"type": "mystery"})
	flush()

	mu.Lock()
	defer mu.Unlock()
	if len(errs) != 1 {
		t.Fatalf("errs = %d, want 1", len(errs))
	}
}

// TS: "reconnects transparently on a transient drop and resumes from the cursor"
func TestChannelReconnectsTransparentlyOnTransientDrop(t *testing.T) {
	c := newConnector()
	var debugMu sync.Mutex
	var debug []string
	ch := newTestChannel(t, c, func(e ChannelDebugEvent) {
		debugMu.Lock()
		debug = append(debug, e.Event)
		debugMu.Unlock()
	})
	mustOpen(t, ch)

	var closesMu sync.Mutex
	var closes []int
	ch.OnClose(func(code int, _ string) {
		closesMu.Lock()
		closes = append(closes, code)
		closesMu.Unlock()
	})

	var text []string
	var textMu sync.Mutex
	ch.On("text-delta", func(e Event) {
		textMu.Lock()
		text = append(text, e.Message.(fakeFrame).delta)
		textMu.Unlock()
	})

	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "one"}, 1)
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "two"}, 2)
	flush()

	c.current().drop()
	waitFor(t, time.Second, func() bool { return c.count() == 2 })

	waitFor(t, time.Second, func() bool {
		debugMu.Lock()
		defer debugMu.Unlock()
		for _, e := range debug {
			if e == DebugReconnected {
				return true
			}
		}
		return false
	})

	want := []string{`{"type":"resume","lastSeenEventId":2}`}
	if got := c.current().Sent(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sent on new socket = %v, want %v", got, want)
	}
	closesMu.Lock()
	if len(closes) != 0 {
		t.Fatalf("closes = %v, want none (transient drop must not fire OnClose)", closes)
	}
	closesMu.Unlock()

	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "three"}, 3)
	flush()
	textMu.Lock()
	got := append([]string(nil), text...)
	textMu.Unlock()
	if fmt.Sprint(got) != fmt.Sprint([]string{"one", "two", "three"}) {
		t.Fatalf("text = %v", got)
	}
}

// TS: "queues host → bridge sends while disconnected and flushes them on reconnect"
func TestChannelQueuesSendsWhileDisconnected(t *testing.T) {
	c := newConnector()
	// Unlike TS (single-threaded: the drop's close handler only runs
	// reconnectLoop on a later microtask, so the test's synchronous
	// channel.send() is guaranteed to queue first), Go's reconnectLoop starts
	// on its own goroutine immediately and can race the main goroutine's
	// Send() call. Gate every connect attempt after the first behind a
	// release closed only once, so Send() reliably queues before the
	// reconnect completes and flushes. (A channel *variable* reassigned
	// between the initial and reconnect attempts would itself be a data race
	// if read concurrently from the Connect goroutine, so the gate is a
	// single channel closed exactly once, not replaced.)
	gate := make(chan struct{})
	var mu sync.Mutex
	first := true
	var sawReconnectAttempt bool
	ch := NewChannel(ChannelOptions{
		Connect: func(ctx context.Context) (Conn, error) {
			mu.Lock()
			isFirst := first
			first = false
			mu.Unlock()
			if !isFirst {
				<-gate
			}
			return c.connect(ctx)
		},
		Decode:    decodeFake,
		Reconnect: testReconnect(),
		OnDebug: func(e ChannelDebugEvent) {
			if e.Event == DebugReconnectAttempt {
				mu.Lock()
				sawReconnectAttempt = true
				mu.Unlock()
			}
		},
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)

	c.current().drop()
	// reconnect-attempt fires (in reconnectLoop, before the gated Connect
	// call) only after onDrop has already set the channel disconnected, so
	// waiting for it — rather than calling Send() immediately after drop() —
	// avoids racing the reader goroutine that notices the drop. TS does not
	// need this: the fake socket's drop() synchronously invokes the close
	// handler before the test's next line runs.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawReconnectAttempt
	})
	if err := ch.Send(AbortCommand{}); err != nil {
		t.Fatalf("Send while disconnected must not error: %v", err)
	}
	close(gate)
	waitFor(t, time.Second, func() bool { return c.count() == 2 })
	flush()

	want := []string{
		`{"type":"resume","lastSeenEventId":0}`,
		`{"type":"abort"}`,
	}
	if got := c.current().Sent(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sent = %v, want %v", got, want)
	}
}

// TS: "fires onClose on a host-initiated close (terminal)"
func TestChannelFiresOnCloseOnHostInitiatedClose(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	var mu sync.Mutex
	var closes []int
	ch.OnClose(func(code int, _ string) {
		mu.Lock()
		closes = append(closes, code)
		mu.Unlock()
	})
	ch.Close()
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(closes) == 1
	})

	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(closes) != fmt.Sprint([]int{1000}) {
		t.Fatalf("closes = %v, want [1000]", closes)
	}
	if !ch.IsClosed() {
		t.Fatal("IsClosed() = false, want true")
	}
}

// TS: "treats a drop after beginClose as terminal, not a reconnect"
func TestChannelDropAfterBeginCloseIsTerminal(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)

	var mu sync.Mutex
	var closes []int
	ch.OnClose(func(code int, _ string) {
		mu.Lock()
		closes = append(closes, code)
		mu.Unlock()
	})

	ch.BeginClose()
	c.current().drop(1000)
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(closes) == 1
	})

	if c.count() != 1 {
		t.Fatalf("connector.count() = %d, want 1 (no reconnect attempted)", c.count())
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(closes) != fmt.Sprint([]int{1000}) {
		t.Fatalf("closes = %v, want [1000]", closes)
	}
}

// TS: "gives up and fires onClose once the reconnect budget is exhausted"
func TestChannelGivesUpAfterReconnectBudgetExhausted(t *testing.T) {
	var calls int
	var first *fakeConn
	var mu sync.Mutex
	ch := NewChannel(ChannelOptions{
		Connect: func(context.Context) (Conn, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				first = newFakeConn()
				return first, nil
			}
			return nil, errors.New("connect refused")
		},
		Decode:    decodeFake,
		Reconnect: ReconnectOptions{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxElapsed: 30 * time.Millisecond},
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)

	var closes []int
	var closesMu sync.Mutex
	ch.OnClose(func(code int, _ string) {
		closesMu.Lock()
		closes = append(closes, code)
		closesMu.Unlock()
	})
	first.drop()
	waitFor(t, 2*time.Second, func() bool {
		closesMu.Lock()
		defer closesMu.Unlock()
		return len(closes) == 1
	})
	closesMu.Lock()
	got := closes[0]
	closesMu.Unlock()
	if got != 1006 {
		t.Fatalf("close code = %d, want 1006", got)
	}
}

// TS: "bounds a hanging reconnect connection by maxElapsedMs"
func TestChannelBoundsHangingReconnectByMaxElapsed(t *testing.T) {
	first := newFakeConn()
	var calls int
	var mu sync.Mutex
	var signals []context.Context
	ch := NewChannel(ChannelOptions{
		Connect: func(ctx context.Context) (Conn, error) {
			mu.Lock()
			calls++
			n := calls
			signals = append(signals, ctx)
			mu.Unlock()
			if n == 1 {
				return first, nil
			}
			<-make(chan struct{}) // never resolves until ctx is canceled by the caller
			return nil, ctx.Err()
		},
		Decode:    decodeFake,
		Reconnect: ReconnectOptions{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxElapsed: 20 * time.Millisecond},
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)

	var closes []int
	var closesMu sync.Mutex
	ch.OnClose(func(code int, _ string) {
		closesMu.Lock()
		closes = append(closes, code)
		closesMu.Unlock()
	})
	first.drop()
	waitFor(t, 2*time.Second, func() bool {
		closesMu.Lock()
		defer closesMu.Unlock()
		return fmt.Sprint(closes) == fmt.Sprint([]int{1006})
	})
	mu.Lock()
	sig := signals[1]
	mu.Unlock()
	waitFor(t, time.Second, func() bool { return sig.Err() != nil })
}

// TS: "aborts an active connection when the channel is torn down"
func TestChannelAbortsActiveConnectionOnTeardown(t *testing.T) {
	first := newFakeConn()
	var calls int
	var mu sync.Mutex
	var reconnectCtx context.Context
	ch := NewChannel(ChannelOptions{
		Connect: func(ctx context.Context) (Conn, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				return first, nil
			}
			mu.Lock()
			reconnectCtx = ctx
			mu.Unlock()
			<-make(chan struct{})
			return nil, ctx.Err()
		},
		Decode:    decodeFake,
		Reconnect: ReconnectOptions{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxElapsed: time.Second},
	})
	mustOpen(t, ch)

	first.drop()
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reconnectCtx != nil
	})
	ch.Close()
	flush()

	mu.Lock()
	sig := reconnectCtx
	mu.Unlock()
	if sig.Err() == nil {
		t.Fatal("reconnect ctx not canceled after Close")
	}
	if !ch.IsClosed() {
		t.Fatal("IsClosed() = false")
	}
}

// TS: "passes a distinct abort signal to each connection attempt"
func TestChannelDistinctContextPerAttempt(t *testing.T) {
	c := newConnector()
	var ctxs []context.Context
	var errAtCall []error
	var mu sync.Mutex
	ch := NewChannel(ChannelOptions{
		Connect: func(ctx context.Context) (Conn, error) {
			mu.Lock()
			// Record ctx.Err() synchronously, in the same call that hands it
			// out — Channel.Open/reconnectLoop both cancel a completed
			// attempt's ctx immediately once ConnectFunc returns (a Go-only
			// cleanup of context.WithCancelCause's resources; TS's
			// AbortController is simply never aborted on success), so
			// checking it after the fact would be racing that cancellation.
			ctxs = append(ctxs, ctx)
			errAtCall = append(errAtCall, ctx.Err())
			mu.Unlock()
			return c.connect(ctx)
		},
		Decode:    decodeFake,
		Reconnect: testReconnect(),
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)
	c.current().drop()
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(ctxs) == 2
	})
	mu.Lock()
	defer mu.Unlock()
	if ctxs[0] == ctxs[1] {
		t.Fatal("expected distinct contexts per attempt")
	}
	for i, err := range errAtCall {
		if err != nil {
			t.Fatalf("ctxs[%d] already canceled when handed to Connect: %v", i, err)
		}
	}
}

// TS: "seeds lastSeenEventId and advances it as events arrive"
func TestChannelSeedsAndAdvancesLastSeenEventID(t *testing.T) {
	c := newConnector()
	ch := NewChannel(ChannelOptions{Connect: c.connect, Decode: decodeFake, InitialLastSeenEventID: 7})
	t.Cleanup(ch.Close)
	if ch.LastSeenEventID() != 7 {
		t.Fatalf("LastSeenEventID = %v, want 7", ch.LastSeenEventID())
	}
	mustOpen(t, ch)
	ch.On("text-delta", func(Event) {})
	c.current().deliver(map[string]any{"type": "text-delta", "id": "m", "delta": "x"}, 9)
	waitFor(t, time.Second, func() bool { return ch.LastSeenEventID() == 9 })
}

// TS: "open({ resume: true }) sends a resume frame with the seeded cursor"
func TestChannelOpenResumeSendsResumeFrame(t *testing.T) {
	c := newConnector()
	ch := NewChannel(ChannelOptions{Connect: c.connect, Decode: decodeFake, InitialLastSeenEventID: 4})
	t.Cleanup(ch.Close)
	if err := ch.Open(context.Background(), true); err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := `{"type":"resume","lastSeenEventId":4}`
	found := false
	for _, s := range c.current().Sent() {
		if s == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("sent = %v, want to contain %q", c.current().Sent(), want)
	}
}

// TS: "open() without resume sends no resume frame"
func TestChannelOpenWithoutResumeSendsNothing(t *testing.T) {
	c := newConnector()
	ch := newTestChannel(t, c, nil)
	mustOpen(t, ch)
	if got := c.current().Sent(); len(got) != 0 {
		t.Fatalf("sent = %v, want none", got)
	}
}

// TS: "routes sandbox-log/debug-event frames to onDiagnostic, not type listeners"
func TestChannelRoutesDiagnosticsToOnDiagnostic(t *testing.T) {
	c := newConnector()
	var mu sync.Mutex
	var diagnostics []string
	ch := NewChannel(ChannelOptions{
		Connect: c.connect,
		Decode:  decodeFake,
		OnDiagnostic: func(msg OutboundMessage) {
			mu.Lock()
			diagnostics = append(diagnostics, msg.FrameType())
			mu.Unlock()
		},
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)

	var leaked []Event
	ch.On("sandbox-log", func(e Event) {
		mu.Lock()
		leaked = append(leaked, e)
		mu.Unlock()
	})
	var text []string
	ch.On("text-delta", func(e Event) {
		mu.Lock()
		text = append(text, e.Message.(fakeFrame).delta)
		mu.Unlock()
	})

	c.current().deliver(map[string]any{"type": "sandbox-log", "source": "bridge", "stream": "stdout", "line": "hi"}, 1)
	c.current().deliver(map[string]any{"type": "debug-event", "level": "info", "subsystem": "bridge.turn", "message": "started"}, 2)
	c.current().deliver(map[string]any{"type": "text-delta", "id": "a", "delta": "x"}, 3)
	flush()

	mu.Lock()
	defer mu.Unlock()
	want := []string{"sandbox-log", "debug-event"}
	if fmt.Sprint(diagnostics) != fmt.Sprint(want) {
		t.Fatalf("diagnostics = %v, want %v", diagnostics, want)
	}
	if len(leaked) != 0 {
		t.Fatalf("leaked = %v, want none", leaked)
	}
	if fmt.Sprint(text) != fmt.Sprint([]string{"x"}) {
		t.Fatalf("text = %v", text)
	}
	if ch.LastSeenEventID() != 3 {
		t.Fatalf("LastSeenEventID = %v, want 3 (diagnostics still advance the cursor)", ch.LastSeenEventID())
	}
}

// TS: "reports error frames to onBridgeError and still dispatches them normally"
func TestChannelReportsErrorFramesToOnBridgeError(t *testing.T) {
	c := newConnector()
	var mu sync.Mutex
	var errs []any
	ch := NewChannel(ChannelOptions{
		Connect: c.connect,
		Decode:  decodeFake,
		OnBridgeError: func(p *harness.ErrorPart) {
			if p != nil {
				mu.Lock()
				errs = append(errs, p.Error)
				mu.Unlock()
			}
		},
	})
	t.Cleanup(ch.Close)
	mustOpen(t, ch)

	var listenerEvents int
	ch.On("error", func(Event) {
		mu.Lock()
		listenerEvents++
		mu.Unlock()
	})
	c.current().deliver(map[string]any{
		"type":  "error",
		"error": map[string]any{"name": "Error", "message": "boom", "stack": "Error: boom"},
	}, 1)
	flush()

	mu.Lock()
	defer mu.Unlock()
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want 1 entry", errs)
	}
	if listenerEvents != 1 {
		t.Fatalf("listenerEvents = %d, want 1", listenerEvents)
	}
	if ch.LastSeenEventID() != 1 {
		t.Fatalf("LastSeenEventID = %v, want 1", ch.LastSeenEventID())
	}
}
