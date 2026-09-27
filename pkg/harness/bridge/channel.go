package bridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// ConnectFunc opens a fresh connection to the bridge and returns once it is
// ready to carry frames (after any adapter handshake such as waiting for
// bridge-hello). Channel calls it once from Open and again on every transient
// reconnect. ctx (the TS abortSignal, d975097) is canceled when the attempt
// is aborted (Close, BeginClose, Suspend, reconnect deadline) and also once
// ConnectFunc has returned, so it must only scope connection establishment,
// never the lifetime of the returned Conn.
type ConnectFunc func(ctx context.Context) (Conn, error)

// ReconnectOptions configures reconnection after an established connection
// drops (c0e5d1d). Zero fields use the defaults.
type ReconnectOptions struct {
	// MaxElapsed is the total reconnect budget, including connection
	// establishment and backoff delays. Default 30s.
	MaxElapsed time.Duration
	// InitialDelay is the first backoff delay. Default 50ms.
	InitialDelay time.Duration
	// MaxDelay is the backoff ceiling. Default 2s.
	MaxDelay time.Duration
}

// Channel debug event names.
const (
	DebugReconnectAttempt = "reconnect-attempt"
	DebugReconnected      = "reconnected"
	DebugReconnectFailed  = "reconnect-failed"
)

// ChannelDebugEvent is a connection lifecycle diagnostic (TS
// `SandboxChannelDebugEvent`). Attempt is set for reconnect-attempt and
// reconnected; Attempts and Cause for reconnect-failed.
type ChannelDebugEvent struct {
	Event           string
	Attempt         int
	Attempts        int
	LastSeenEventID float64
	Cause           error
}

// Close reasons passed to OnClose handlers.
const (
	CloseReasonClosed          = "closed"
	CloseReasonSuspended       = "suspended"
	CloseReasonReconnectFailed = "reconnect failed"
)

// DefaultSelectiveFlushGrace is how long buffered events of a type nobody
// listens to may hold back later, subscribed events. See ChannelOptions.
const DefaultSelectiveFlushGrace = 5 * time.Millisecond

// ChannelOptions configures NewChannel.
type ChannelOptions struct {
	// Connect opens a connection (required).
	Connect ConnectFunc
	// Decode parses one inbound frame (default DecodeOutbound). Adapters with
	// extra outbound frames wrap DecodeOutbound.
	Decode func(data []byte) (OutboundMessage, *float64, error)
	// Reconnect tunes the reconnect budget and backoff.
	Reconnect ReconnectOptions
	// OnDebug receives connection lifecycle events.
	OnDebug func(ChannelDebugEvent)
	// OnDiagnostic receives `sandbox-log` and `debug-event` frames. They are
	// never delivered to type listeners, so they stay off the consumer stream.
	OnDiagnostic func(OutboundMessage)
	// OnBridgeError observes `error` frames (including malformed frames);
	// they are still delivered to `error` listeners.
	OnBridgeError func(*harness.ErrorPart)
	// InitialLastSeenEventID seeds the cursor, e.g. with the value persisted
	// by a prior process, so Open(ctx, true) replays only later events (the
	// cross-process attach handshake).
	InitialLastSeenEventID float64
	// SelectiveFlushGrace replaces the TS "current task" window: after a
	// listener is registered, buffered events of types with no listener keep
	// their place in the ordered buffer for this long (re-armed on every
	// registration) before they are set aside so they cannot block subscribed
	// types. Default DefaultSelectiveFlushGrace. For a strict guarantee across
	// a set of registrations use BeginListenerAttachment.
	SelectiveFlushGrace time.Duration
}

// Event is one frame delivered to a Channel listener.
type Event struct {
	Message OutboundMessage
	// Seq is the bridge event cursor; HasSeq is false for control frames.
	Seq    float64
	HasSeq bool

	ch *Channel
}

// Type returns the frame discriminator.
func (e Event) Type() string { return e.Message.FrameType() }

// PinCheckpoint pins the suspension cursor to this event: a later Suspend
// returns this event's seq even if later events were already dispatched, so
// the next process replays them. The returned release function unpins it
// (only if no newer pin replaced it). Returns nil for events without a seq.
// Mirrors TS `pinSandboxChannelEventCheckpoint`.
func (e Event) PinCheckpoint() (release func()) {
	if !e.HasSeq || e.ch == nil {
		return nil
	}
	return e.ch.pinCheckpointAt(e.Seq)
}

// PinEventCheckpoint is the free-function form of Event.PinCheckpoint.
func PinEventCheckpoint(e Event) func() { return e.PinCheckpoint() }

// PinCheckpoint pins the suspension cursor to the channel's most recently
// observed event (LastSeenEventID), without requiring the caller to hold a
// specific Event value. Used by an adapter's PromptControl (harness.
// CheckpointPinner) whose wireTurn forwards parts without retaining each
// individual bridge.Event — see Event.PinCheckpoint for full pinning
// semantics, which this shares exactly, just seeded from the channel's
// current cursor instead of one particular event's seq.
func (c *Channel) PinCheckpoint() (release func()) {
	c.mu.Lock()
	seq := c.lastSeen
	c.mu.Unlock()
	return c.pinCheckpointAt(seq)
}

func (c *Channel) pinCheckpointAt(seq float64) (release func()) {
	pin := &pinnedCursor{eventID: seq}
	c.mu.Lock()
	c.pinned = pin
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		if c.pinned == pin {
			c.pinned = nil
		}
		c.mu.Unlock()
	}
}

type pinnedCursor struct{ eventID float64 }

type listener struct{ fn func(Event) }

type bufferedEvent struct {
	ev        Event
	listeners []*listener // nil: resolve at delivery time
}

type reconnectHandler struct{ fn func() }

type cancelHandle struct{ cancel context.CancelCauseFunc }

// ErrChannelAborted is the abort reason used when the channel tears down an
// in-flight connection attempt.
var ErrChannelAborted = errors.New("SandboxChannel connection aborted")

// errReconnectDeadline is the cause of an exhausted reconnect budget.
var errReconnectDeadline = errors.New("Reconnect deadline expired")

// Channel is the host-side typed wrapper around the bridge connection (TS
// `SandboxChannel`).
//
// It buffers inbound frames in arrival order while listeners attach, so
// callers neither miss nor reorder early frames; registering a listener
// replays the ordered buffered prefix. Buffered events of a type nobody
// listens to are set aside after SelectiveFlushGrace so they cannot block
// subscribed types indefinitely (7115a3f); they are replayed when a listener
// for their type registers. Listeners are never invoked concurrently, but may
// run on any goroutine (the dispatch goroutine, or the goroutine registering a
// listener). Inbound processing is serialized, so a close that follows the
// final frame is handled after that frame is dispatched.
//
// The channel survives transient disconnects: the bridge keeps running and
// logs events under a monotonic seq; on an unexpected drop the channel calls
// Connect again with backoff and sends `resume{lastSeenEventId}` so the
// bridge replays everything newer. OnClose fires only after a host-initiated
// close, a suspend, or once the reconnect budget is exhausted.
type Channel struct {
	connect      ConnectFunc
	decode       func([]byte) (OutboundMessage, *float64, error)
	onDebug      func(ChannelDebugEvent)
	onDiagnostic func(OutboundMessage)
	onBridgeErr  func(*harness.ErrorPart)
	maxElapsed   time.Duration
	initialDelay time.Duration
	maxDelay     time.Duration
	grace        time.Duration

	mu sync.Mutex
	// listeners
	listeners      map[string][]*listener
	listenerTypes  []string
	buffered       []*bufferedEvent
	bufferedByType map[string][]Event
	attachDepth    int
	flushing       bool
	reqTypes       []string
	reqOrdered     bool
	reqSelective   bool
	selScheduled   bool
	selTimer       *time.Timer
	// lifecycle
	closeHandlers     []func(code int, reason string)
	reconnectHandlers []*reconnectHandler
	ws                Conn
	connected         bool
	closing           bool
	suspended         bool
	terminal          bool
	pinned            *pinnedCursor
	lastSeen          float64
	pendingSends      [][]byte
	activeConnect     *cancelHandle
	reconnectAbort    *cancelHandle
	done              chan struct{}

	sendMu sync.Mutex

	qmu      sync.Mutex
	queue    []func()
	qRunning bool
}

// NewChannel creates an unopened channel.
func NewChannel(opts ChannelOptions) *Channel {
	c := &Channel{
		connect:        opts.Connect,
		decode:         opts.Decode,
		onDebug:        opts.OnDebug,
		onDiagnostic:   opts.OnDiagnostic,
		onBridgeErr:    opts.OnBridgeError,
		maxElapsed:     opts.Reconnect.MaxElapsed,
		initialDelay:   opts.Reconnect.InitialDelay,
		maxDelay:       opts.Reconnect.MaxDelay,
		grace:          opts.SelectiveFlushGrace,
		listeners:      map[string][]*listener{},
		bufferedByType: map[string][]Event{},
		lastSeen:       opts.InitialLastSeenEventID,
		done:           make(chan struct{}),
	}
	if c.decode == nil {
		c.decode = DecodeOutbound
	}
	if c.maxElapsed <= 0 {
		c.maxElapsed = 30 * time.Second
	}
	if c.initialDelay <= 0 {
		c.initialDelay = 50 * time.Millisecond
	}
	if c.maxDelay <= 0 {
		c.maxDelay = 2 * time.Second
	}
	if c.grace <= 0 {
		c.grace = DefaultSelectiveFlushGrace
	}
	return c
}

// LastSeenEventID is the highest bridge event seq observed. Persist it so a
// future process can seed InitialLastSeenEventID and attach.
func (c *Channel) LastSeenEventID() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSeen
}

// Open establishes the initial connection with a single attempt; startup
// failures are returned so the caller can fail cleanly. Reconnect retries
// apply only to drops after a successful Open. With resume, the channel sends
// `resume{lastSeenEventId}` right after connecting so a bridge that is
// already mid-session replays everything past the seeded cursor.
func (c *Channel) Open(ctx context.Context, resume bool) error {
	c.mu.Lock()
	if c.terminal {
		c.mu.Unlock()
		return errors.New("SandboxChannel: cannot open a closed channel.")
	}
	connectCtx, cancel := context.WithCancelCause(ctx)
	handle := &cancelHandle{cancel: cancel}
	c.activeConnect = handle
	c.mu.Unlock()

	conn, err := c.connectOnce(connectCtx)

	c.mu.Lock()
	if c.activeConnect == handle {
		c.activeConnect = nil
	}
	if err != nil {
		c.mu.Unlock()
		cancel(nil)
		return err
	}
	c.ws = conn
	c.connected = true
	c.wire(conn)
	var seed float64 = c.lastSeen
	c.mu.Unlock()
	cancel(nil)

	if resume {
		c.rawSend(mustMarshalInbound(ResumeCommand{LastSeenEventID: seed}))
	}
	// Matches TS `open()`: pendingSends is only flushed by reconnectLoop, never
	// here. A Send() issued before Open() completes stays queued until the
	// first reconnect (a quirk inherited from TS, not fixed here per the 1:1
	// parity rule).
	return nil
}

// On registers a listener for one frame type and returns its unsubscribe
// function. Frames of that type already buffered in order are delivered
// before On returns unless another goroutine is delivering (then that
// goroutine delivers them, still in order).
func (c *Channel) On(frameType string, fn func(Event)) (unsubscribe func()) {
	l := &listener{fn: fn}
	c.mu.Lock()
	if _, ok := c.listeners[frameType]; !ok {
		c.listenerTypes = append(c.listenerTypes, frameType)
	}
	c.listeners[frameType] = append(c.listeners[frameType], l)
	c.captureBufferedListeners(frameType)
	attaching := c.attachDepth > 0
	c.mu.Unlock()

	unsubscribe = func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		set := c.listeners[frameType]
		for i, v := range set {
			if v == l {
				c.listeners[frameType] = append(set[:i:i], set[i+1:]...)
				return
			}
		}
	}
	if attaching {
		return unsubscribe
	}
	c.drain([]string{frameType}, true, false)
	c.scheduleSelectiveFlush(true)
	return unsubscribe
}

// BeginListenerAttachment holds buffered delivery while a related set of
// listeners is registered, including across asynchronous work. Call the
// returned function when registration is complete: buffered events are then
// replayed in arrival order. Mirrors TS `beginListenerAttachment`.
func (c *Channel) BeginListenerAttachment() (finish func()) {
	c.mu.Lock()
	c.attachDepth++
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.attachDepth--
			if c.attachDepth > 0 {
				c.mu.Unlock()
				return
			}
			types := append([]string(nil), c.listenerTypes...)
			c.mu.Unlock()
			c.drain(types, true, false)
			c.scheduleSelectiveFlush(false)
		})
	}
}

// OnClose registers a handler for the terminal close. Code/reason: 1000
// "closed" (Close), 1000 "suspended" (Suspend), 1006 "reconnect failed", or
// the socket's close after BeginClose.
func (c *Channel) OnClose(handler func(code int, reason string)) {
	c.mu.Lock()
	c.closeHandlers = append(c.closeHandlers, handler)
	c.mu.Unlock()
}

// OnReconnect registers a handler run after each successful reconnect (after
// the resume frame and queued sends went out). Returns an unsubscribe func.
func (c *Channel) OnReconnect(handler func()) (unsubscribe func()) {
	h := &reconnectHandler{fn: handler}
	c.mu.Lock()
	c.reconnectHandlers = append(c.reconnectHandlers, h)
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, v := range c.reconnectHandlers {
			if v == h {
				c.reconnectHandlers = append(c.reconnectHandlers[:i:i], c.reconnectHandlers[i+1:]...)
				return
			}
		}
	}
}

// Send serializes and sends a host->bridge frame. While disconnected the
// frame is queued and flushed after the next reconnect. It fails only once
// the channel is closed.
func (c *Channel) Send(cmd InboundCommand) error {
	c.mu.Lock()
	terminal := c.terminal
	c.mu.Unlock()
	if terminal {
		return fmt.Errorf("SandboxChannel: cannot send %s — channel is closed.", cmd.FrameType())
	}
	data, err := MarshalInbound(cmd)
	if err != nil {
		return err
	}
	c.rawSend(data)
	return nil
}

// BeginClose marks that the host is tearing the session down: the next
// socket close is terminal rather than triggering a reconnect. Call it before
// sending `stop` / `destroy`.
func (c *Channel) BeginClose() {
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	c.abortActiveConnect()
}

// Close closes the channel (terminal; OnClose fires with 1000 "closed").
func (c *Channel) Close() {
	c.mu.Lock()
	if c.terminal {
		c.mu.Unlock()
		return
	}
	c.closing = true
	conn := c.ws
	c.mu.Unlock()
	c.abortActiveConnect()
	c.enqueue(func() { c.finalizeClose(1000, CloseReasonClosed) })
	if conn != nil {
		_ = conn.Close()
	}
}

// Suspend gracefully suspends at a slice boundary: inbound frames are
// ignored from now on (the cursor freezes at the last delivered event),
// frames already queued are dispatched, then the socket closes and OnClose
// fires with reason "suspended". The returned channel yields the final
// cursor: the pinned checkpoint if one is set, else LastSeenEventID. The
// bridge keeps the turn running and replays the tail to the next process.
// The result is delivered asynchronously, so Suspend may be called from a
// listener.
func (c *Channel) Suspend() <-chan float64 {
	result := make(chan float64, 1)
	c.mu.Lock()
	pinned := c.pinned
	cursor := func() float64 {
		if pinned != nil {
			return pinned.eventID
		}
		return c.lastSeen
	}
	if c.terminal {
		result <- cursor()
		c.mu.Unlock()
		return result
	}
	c.suspended = true
	c.closing = true
	var once sync.Once
	c.closeHandlers = append(c.closeHandlers, func(int, string) {
		once.Do(func() {
			c.mu.Lock()
			v := cursor()
			c.mu.Unlock()
			result <- v
		})
	})
	c.mu.Unlock()
	c.abortActiveConnect()
	c.enqueue(func() {
		c.mu.Lock()
		conn := c.ws
		c.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		c.finalizeClose(1000, CloseReasonSuspended)
	})
	return result
}

// IsClosed reports whether the channel is terminally closed.
func (c *Channel) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminal
}

// Done is closed once the channel is terminally closed.
func (c *Channel) Done() <-chan struct{} { return c.done }

// ─── internals ─────────────────────────────────────────────────────────

// wire starts the reader for conn. c.mu must be held.
func (c *Channel) wire(conn Conn) {
	go func() {
		for {
			data, err := conn.Receive()
			if err != nil {
				code, reason := 1006, "socket error"
				var ce *CloseError
				if errors.As(err, &ce) {
					code, reason = ce.Code, ce.Reason
				}
				c.onDrop(conn, code, reason)
				return
			}
			c.mu.Lock()
			suspended := c.suspended
			c.mu.Unlock()
			if suspended {
				continue
			}
			c.enqueue(func() { c.handleIncoming(data) })
		}
	}()
}

func (c *Channel) onDrop(conn Conn, code int, reason string) {
	c.mu.Lock()
	if conn != c.ws {
		c.mu.Unlock()
		return
	}
	c.connected = false
	closing := c.closing
	c.mu.Unlock()
	if closing {
		c.enqueue(func() { c.finalizeClose(code, reason) })
	} else {
		c.enqueue(c.reconnectLoop)
	}
}

func (c *Channel) reconnectLoop() {
	c.mu.Lock()
	if c.terminal || c.closing {
		c.mu.Unlock()
		return
	}
	rctx, rcancel := context.WithCancelCause(context.Background())
	rhandle := &cancelHandle{cancel: rcancel}
	c.reconnectAbort = rhandle
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.reconnectAbort == rhandle {
			c.reconnectAbort = nil
		}
		c.mu.Unlock()
		rcancel(nil)
	}()

	deadline := time.Now().Add(c.maxElapsed)
	attempt := 0
	delay := c.initialDelay
	for {
		if c.stopping() {
			return
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			c.failReconnect(attempt, errReconnectDeadline)
			return
		}
		attempt++
		c.debug(ChannelDebugEvent{Event: DebugReconnectAttempt, Attempt: attempt, LastSeenEventID: c.LastSeenEventID()})

		actx, acancel := context.WithCancelCause(rctx)
		ahandle := &cancelHandle{cancel: acancel}
		timer := time.AfterFunc(remaining, func() { acancel(errReconnectDeadline) })
		c.mu.Lock()
		c.activeConnect = ahandle
		c.mu.Unlock()

		conn, err := c.connectOnce(actx)

		timer.Stop()
		c.mu.Lock()
		if c.activeConnect == ahandle {
			c.activeConnect = nil
		}
		if err == nil {
			if c.terminal || c.closing {
				c.mu.Unlock()
				acancel(nil)
				_ = conn.Close()
				return
			}
			c.ws = conn
			c.connected = true
			c.wire(conn)
			seed := c.lastSeen
			c.mu.Unlock()
			acancel(nil)
			// Replay everything not yet seen, then flush frames produced while
			// disconnected.
			c.rawSend(mustMarshalInbound(ResumeCommand{LastSeenEventID: seed}))
			c.flushPending()
			c.mu.Lock()
			handlers := append([]*reconnectHandler(nil), c.reconnectHandlers...)
			c.mu.Unlock()
			for _, h := range handlers {
				h.fn()
			}
			c.debug(ChannelDebugEvent{Event: DebugReconnected, Attempt: attempt, LastSeenEventID: c.LastSeenEventID()})
			return
		}
		c.mu.Unlock()
		acancel(nil)
		if c.stopping() || rctx.Err() != nil {
			return
		}
		remainingAfter := time.Until(deadline)
		if remainingAfter <= 0 {
			c.failReconnect(attempt, err)
			return
		}
		Sleep(rctx, min(delay, remainingAfter))
		delay = min(time.Duration(float64(delay)*1.5), c.maxDelay)
	}
}

func (c *Channel) stopping() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminal || c.closing
}

// connectOnce runs ConnectFunc bounded by ctx. A connection that arrives
// after ctx was aborted is closed.
func (c *Channel) connectOnce(ctx context.Context) (Conn, error) {
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	type result struct {
		conn Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := c.connect(ctx)
		ch <- result{conn, err}
	}()
	select {
	case r := <-ch:
		if ctx.Err() != nil {
			if r.conn != nil {
				_ = r.conn.Close()
			}
			return nil, context.Cause(ctx)
		}
		if r.err == nil && r.conn == nil {
			return nil, errors.New("SandboxChannel: connect returned no connection")
		}
		return r.conn, r.err
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, context.Cause(ctx)
	}
}

func (c *Channel) abortActiveConnect() {
	c.mu.Lock()
	active := c.activeConnect
	c.activeConnect = nil
	reconnect := c.reconnectAbort
	c.mu.Unlock()
	if active != nil {
		active.cancel(ErrChannelAborted)
	}
	if reconnect != nil {
		reconnect.cancel(ErrChannelAborted)
	}
}

func (c *Channel) failReconnect(attempts int, cause error) {
	c.finalizeClose(1006, CloseReasonReconnectFailed)
	c.debug(ChannelDebugEvent{Event: DebugReconnectFailed, Attempts: attempts, LastSeenEventID: c.LastSeenEventID(), Cause: cause})
}

func (c *Channel) debug(ev ChannelDebugEvent) {
	if c.onDebug != nil {
		c.onDebug(ev)
	}
}

// rawSend mirrors TS `rawSend`/`ws.send()`: while disconnected the frame is
// queued; once connected it is written and a write failure is silently
// dropped rather than requeued. TS's `ws` library swallows a send on a
// closing/closed socket when no callback is given (no throw, no retry); the
// independent 'close'/'error' event still fires and drives the reconnect
// loop, so a dropped frame does not go unnoticed for long. Go mirrors this:
// a failed Send is not requeued and does not force-close the socket — the
// reader goroutine's Receive() error is what detects the drop.
func (c *Channel) rawSend(data []byte) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.mu.Lock()
	if !c.connected || c.ws == nil {
		c.pendingSends = append(c.pendingSends, data)
		c.mu.Unlock()
		return
	}
	conn := c.ws
	c.mu.Unlock()
	_ = conn.Send(data)
}

// flushPending mirrors TS `flushPending`: it sends every queued frame without
// checking for errors or requeueing on failure (see rawSend).
func (c *Channel) flushPending() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.mu.Lock()
	if !c.connected || c.ws == nil || len(c.pendingSends) == 0 {
		c.mu.Unlock()
		return
	}
	queued := c.pendingSends
	c.pendingSends = nil
	conn := c.ws
	c.mu.Unlock()
	for _, data := range queued {
		_ = conn.Send(data)
	}
}

func (c *Channel) enqueue(work func()) {
	c.qmu.Lock()
	c.queue = append(c.queue, work)
	if c.qRunning {
		c.qmu.Unlock()
		return
	}
	c.qRunning = true
	c.qmu.Unlock()
	go c.runQueue()
}

func (c *Channel) runQueue() {
	for {
		c.qmu.Lock()
		if len(c.queue) == 0 {
			c.qRunning = false
			c.qmu.Unlock()
			return
		}
		work := c.queue[0]
		c.queue[0] = nil
		c.queue = c.queue[1:]
		c.qmu.Unlock()
		work()
	}
}

func (c *Channel) handleIncoming(data []byte) {
	msg, seq, err := c.decode(data)
	if err == nil {
		ev := Event{Message: msg, ch: c}
		if seq != nil {
			ev.Seq, ev.HasSeq = *seq, true
		}
		c.dispatch(ev)
	} else {
		c.dispatch(Event{Message: StreamPartFrame{Part: &harness.ErrorPart{Error: err}}, ch: c})
	}
	if seq != nil {
		c.mu.Lock()
		if *seq > c.lastSeen {
			c.lastSeen = *seq
		}
		c.mu.Unlock()
	}
}

func (c *Channel) dispatch(ev Event) {
	typ := ev.Type()
	if typ == TypeSandboxLog || typ == TypeDebugEvent {
		if c.onDiagnostic != nil {
			c.onDiagnostic(ev.Message)
		}
		return
	}
	if typ == harness.PartTypeError && c.onBridgeErr != nil {
		if f, ok := ev.Message.(StreamPartFrame); ok {
			if p, ok := f.Part.(*harness.ErrorPart); ok {
				c.onBridgeErr(p)
			}
		}
	}
	c.mu.Lock()
	b := &bufferedEvent{ev: ev}
	if set := c.listeners[typ]; len(set) > 0 {
		b.listeners = append([]*listener(nil), set...)
	}
	c.buffered = append(c.buffered, b)
	c.mu.Unlock()
	c.drain(nil, true, false)
	c.scheduleSelectiveFlush(false)
}

// captureBufferedListeners pins the current listeners of typ onto buffered
// events of that type that have none yet. c.mu must be held.
func (c *Channel) captureBufferedListeners(typ string) {
	set := c.listeners[typ]
	if len(set) == 0 {
		return
	}
	for _, b := range c.buffered {
		if b.listeners == nil && b.ev.Type() == typ {
			b.listeners = append([]*listener(nil), set...)
		}
	}
}

// drain delivers buffered events. Only one goroutine delivers at a time; a
// call made while another delivery is in progress (including a re-entrant
// call from a listener) records its request for the active deliverer.
func (c *Channel) drain(types []string, ordered, selective bool) {
	c.mu.Lock()
	for _, t := range types {
		c.addTypeRequest(t)
	}
	c.reqOrdered = c.reqOrdered || ordered
	c.reqSelective = c.reqSelective || selective
	if c.flushing {
		c.mu.Unlock()
		return
	}
	c.flushing = true
	defer func() {
		c.flushing = false
		c.mu.Unlock()
	}()

	deliver := func(ls []*listener, ev Event) {
		c.mu.Unlock()
		defer c.mu.Lock()
		for _, l := range ls {
			l.fn(ev)
		}
	}

	for {
		if c.attachDepth > 0 {
			// Attachment end re-requests everything.
			c.reqTypes, c.reqOrdered, c.reqSelective = nil, false, false
			return
		}
		// Events of a type previously set aside go first: they are older
		// than anything still in the ordered buffer.
		if len(c.reqTypes) > 0 {
			t := c.reqTypes[0]
			queued := c.bufferedByType[t]
			set := c.listeners[t]
			if len(queued) == 0 || len(set) == 0 {
				c.reqTypes = c.reqTypes[1:]
				continue
			}
			ev := queued[0]
			if len(queued) == 1 {
				delete(c.bufferedByType, t)
			} else {
				c.bufferedByType[t] = queued[1:]
			}
			deliver(append([]*listener(nil), set...), ev)
			continue
		}
		if c.reqOrdered || c.reqSelective {
			if len(c.buffered) == 0 {
				c.reqOrdered, c.reqSelective = false, false
				continue
			}
			head := c.buffered[0]
			ls := head.listeners
			if ls == nil {
				ls = append([]*listener(nil), c.listeners[head.ev.Type()]...)
			}
			if len(ls) == 0 {
				if !c.reqSelective {
					c.reqOrdered = false
					continue
				}
				c.buffered = c.buffered[1:]
				t := head.ev.Type()
				c.bufferedByType[t] = append(c.bufferedByType[t], head.ev)
				continue
			}
			c.buffered = c.buffered[1:]
			deliver(ls, head.ev)
			continue
		}
		return
	}
}

func (c *Channel) addTypeRequest(t string) {
	for _, v := range c.reqTypes {
		if v == t {
			return
		}
	}
	c.reqTypes = append(c.reqTypes, t)
}

// scheduleSelectiveFlush arms the grace timer after which unhandled buffered
// types are set aside. rearm restarts a pending timer (listener
// registration), approximating the TS "same task" window.
func (c *Channel) scheduleSelectiveFlush(rearm bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buffered) == 0 || !c.hasListeners() || c.attachDepth > 0 {
		return
	}
	if c.selScheduled {
		if rearm && c.selTimer != nil {
			c.selTimer.Reset(c.grace)
		}
		return
	}
	c.selScheduled = true
	c.selTimer = time.AfterFunc(c.grace, func() {
		c.mu.Lock()
		c.selScheduled = false
		attaching := c.attachDepth > 0
		c.mu.Unlock()
		if attaching {
			return
		}
		c.drain(nil, false, true)
	})
}

func (c *Channel) hasListeners() bool {
	for _, set := range c.listeners {
		if len(set) > 0 {
			return true
		}
	}
	return false
}

func (c *Channel) finalizeClose(code int, reason string) {
	c.abortActiveConnect()
	c.mu.Lock()
	if c.terminal {
		c.mu.Unlock()
		return
	}
	c.terminal = true
	c.connected = false
	handlers := append([]func(int, string){}, c.closeHandlers...)
	close(c.done)
	c.mu.Unlock()
	for _, h := range handlers {
		h(code, reason)
	}
}

func mustMarshalInbound(cmd InboundCommand) []byte {
	data, err := MarshalInbound(cmd)
	if err != nil {
		panic(fmt.Sprintf("harness bridge: marshal %s: %v", cmd.FrameType(), err))
	}
	return data
}
