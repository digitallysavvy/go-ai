package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
)

// Conn is one established host<->bridge socket. Receive blocks until a
// message arrives or the socket fails; Close unblocks a pending Receive.
// Implementations must allow Send and Close to be called concurrently with
// Receive.
type Conn interface {
	Receive() ([]byte, error)
	Send(data []byte) error
	Close() error
}

// CloseError is returned by Conn.Receive when the socket closes. Code follows
// WebSocket close codes: 1000 for a clean close, 1006 for an abnormal one.
type CloseError struct {
	Code   int
	Reason string
	Err    error
}

func (e *CloseError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("bridge socket closed (%d %s): %v", e.Code, e.Reason, e.Err)
	}
	return fmt.Sprintf("bridge socket closed (%d %s)", e.Code, e.Reason)
}

func (e *CloseError) Unwrap() error { return e.Err }

// DefaultWriteTimeout bounds each WebSocket write.
const DefaultWriteTimeout = 10 * time.Second

// wsConn adapts golang.org/x/net/websocket to Conn. x/net has no context
// support, so every blocking call is bounded by deadlines instead.
type wsConn struct {
	ws           *websocket.Conn
	writeTimeout time.Duration
	writeMu      sync.Mutex
	closeOnce    sync.Once
	closeErr     error
}

// Receive reads one text or binary message.
func (c *wsConn) Receive() ([]byte, error) {
	var msg []byte
	if err := websocket.Message.Receive(c.ws, &msg); err != nil {
		if isEOF(err) {
			// x/net reports a received close frame (or an orderly TCP
			// shutdown) as io.EOF; it does not expose the close code.
			return nil, &CloseError{Code: 1000, Err: err}
		}
		return nil, &CloseError{Code: 1006, Reason: "socket error", Err: err}
	}
	return msg, nil
}

// Send writes one text message within the write timeout.
func (c *wsConn) Send(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.writeTimeout > 0 {
		_ = c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout))
		defer func() { _ = c.ws.SetWriteDeadline(time.Time{}) }()
	}
	return websocket.Message.Send(c.ws, string(data))
}

// Close closes the socket. Safe to call more than once.
func (c *wsConn) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.ws.Close() })
	return c.closeErr
}

// DialOptions configures Dial.
type DialOptions struct {
	// Name labels error messages ("claude-code bridge did not send
	// bridge-hello ..."). Default "bridge".
	Name string
	// OpenTimeout bounds the WebSocket upgrade (0: only ctx bounds it).
	OpenTimeout time.Duration
	// WaitForHello makes Dial return only after the bridge's `bridge-hello`
	// frame arrived (Claude Code, OpenCode). Codex/ACP/Deep Agents dial
	// without it and receive the hello as an ordinary frame.
	WaitForHello bool
	// HelloTimeout bounds the wait for bridge-hello after the socket opened.
	HelloTimeout time.Duration
	// HelloDeadline, when set, caps the hello wait at an absolute time (an
	// overall startup budget).
	HelloDeadline time.Time
	// OnHello receives the hello frame.
	OnHello func(*Hello)
	// WriteTimeout bounds each write (default DefaultWriteTimeout).
	WriteTimeout time.Duration
	// Origin is unused: Dial derives the handshake's Origin header from the
	// target URL itself (x/net/websocket requires one; TS's bridge client,
	// dialing through a browser or Node WebSocket client, sets none). Kept
	// as a field for backward compatibility with existing callers; a future
	// release may remove it.
	Origin string
}

// Dial opens a WebSocket to endpoint (URL plus headers) and, with
// WaitForHello, waits for `bridge-hello` before returning, so nothing is sent
// before the end-to-end link is proven live (5cc654f). Because frames are
// read synchronously from the socket, a hello sent the instant the bridge
// accepts cannot be missed. On any failure or ctx cancellation the socket is
// closed and no goroutine is left behind (ab46c30).
func Dial(ctx context.Context, endpoint harness.PortEndpoint, opts DialOptions) (Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, abortError(ctx)
	}
	name := opts.Name
	if name == "" {
		name = "bridge"
	}

	dialCtx := ctx
	if opts.OpenTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeoutCause(ctx, opts.OpenTimeout,
			fmt.Errorf("WebSocket open timed out after %dms", opts.OpenTimeout.Milliseconds()))
		defer cancel()
	}
	ws, err := wsutil.Dial(dialCtx, endpoint.URL, wsutil.DialOptions{Headers: endpoint.Headers})
	if err != nil {
		if ctx.Err() != nil {
			return nil, abortError(ctx)
		}
		if cause := context.Cause(dialCtx); cause != nil && dialCtx.Err() != nil {
			return nil, cause
		}
		return nil, err
	}
	writeTimeout := opts.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout
	}
	conn := &wsConn{ws: ws, writeTimeout: writeTimeout}
	if !opts.WaitForHello {
		if ctx.Err() != nil {
			_ = conn.Close()
			return nil, abortError(ctx)
		}
		return conn, nil
	}

	// Abort: unblock the pending read by expiring its deadline.
	var aborted bool
	var abortMu sync.Mutex
	stop := context.AfterFunc(ctx, func() {
		abortMu.Lock()
		aborted = true
		abortMu.Unlock()
		_ = ws.SetReadDeadline(time.Now())
	})
	defer stop()

	helloTimeout := opts.HelloTimeout
	var helloDeadline time.Time
	if helloTimeout > 0 {
		helloDeadline = time.Now().Add(helloTimeout)
	}
	if !opts.HelloDeadline.IsZero() && (helloDeadline.IsZero() || opts.HelloDeadline.Before(helloDeadline)) {
		helloDeadline = opts.HelloDeadline
		helloTimeout = max(time.Until(helloDeadline), time.Millisecond)
	}
	fail := func(err error) (Conn, error) {
		_ = conn.Close()
		return nil, err
	}
	for {
		abortMu.Lock()
		isAborted := aborted
		if !isAborted {
			_ = ws.SetReadDeadline(helloDeadline)
		}
		abortMu.Unlock()
		if isAborted {
			return fail(abortError(ctx))
		}
		var msg []byte
		err := websocket.Message.Receive(ws, &msg)
		if err != nil {
			if ctx.Err() != nil {
				return fail(abortError(ctx))
			}
			if !helloDeadline.IsZero() && !time.Now().Before(helloDeadline) {
				return fail(fmt.Errorf("%s did not send bridge-hello within %dms", name, helloTimeout.Milliseconds()))
			}
			return fail(fmt.Errorf("%s closed before sending bridge-hello", name))
		}
		hello, ok := parseHello(msg)
		if !ok {
			continue
		}
		if !stop() {
			// ctx fired concurrently; the read deadline may be poisoned.
			return fail(abortError(ctx))
		}
		_ = ws.SetReadDeadline(time.Time{})
		if opts.OnHello != nil {
			opts.OnHello(hello)
		}
		return conn, nil
	}
}

// parseHello returns the frame when it is a JSON object whose type is
// `bridge-hello` (other frames before hello are ignored, as in TS).
func parseHello(msg []byte) (*Hello, bool) {
	var probe struct {
		Type any `json:"type"`
	}
	if json.Unmarshal(msg, &probe) != nil || probe.Type != TypeHello {
		return nil, false
	}
	var hello Hello
	_ = json.Unmarshal(msg, &hello)
	return &hello, true
}

// SupportsUserMessageResponses reports the hello capability
// `experimental_userMessageResponses === true`.
func (h *Hello) SupportsUserMessageResponses() bool {
	return h != nil && h.Capabilities != nil && h.Capabilities.ExperimentalUserMessageResponses != nil &&
		*h.Capabilities.ExperimentalUserMessageResponses
}

// errConnectionAborted is the default abort reason (TS
// "WebSocket connection aborted").
var errConnectionAborted = errors.New("WebSocket connection aborted")

func abortError(ctx context.Context) error {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return errConnectionAborted
}

// OpenOptions configures OpenBridgeWebSocket.
type OpenOptions struct {
	// Name labels error messages (e.g. "claude-code bridge").
	Name string
	// Timeout is the overall handshake budget across attempts.
	Timeout time.Duration
	// OnHello receives each successful hello.
	OnHello func(*Hello)
	// WriteTimeout bounds each write.
	WriteTimeout time.Duration
}

// OpenBridgeWebSocket dials endpoint and waits for bridge-hello, retrying
// with a 250ms*attempt (max 1s) backoff until Timeout. Each attempt's open is
// bounded by min(10s, remaining) and its hello by min(5s, remaining). Mirrors
// the TS Claude Code adapter's `openBridgeWebSocket`.
func OpenBridgeWebSocket(ctx context.Context, endpoint harness.PortEndpoint, opts OpenOptions) (Conn, error) {
	name := opts.Name
	if name == "" {
		name = "bridge"
	}
	deadline := time.Now().Add(opts.Timeout)
	attempt := 0
	var lastErr error
	for ctx.Err() == nil && time.Now().Before(deadline) {
		attempt++
		remaining := max(time.Until(deadline), time.Millisecond)
		conn, err := Dial(ctx, endpoint, DialOptions{
			Name:          name,
			OpenTimeout:   min(10*time.Second, remaining),
			WaitForHello:  true,
			HelloTimeout:  5 * time.Second,
			HelloDeadline: deadline,
			OnHello:       opts.OnHello,
			WriteTimeout:  opts.WriteTimeout,
		})
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, abortError(ctx)
		}
		lastErr = err
		remaining = time.Until(deadline)
		if remaining <= 0 {
			break
		}
		Sleep(ctx, min(time.Duration(attempt)*250*time.Millisecond, time.Second, remaining))
	}
	if ctx.Err() != nil {
		return nil, abortError(ctx)
	}
	lastMsg := "undefined"
	if lastErr != nil {
		lastMsg = lastErr.Error()
	}
	return nil, fmt.Errorf("%s did not complete WebSocket handshake within %dms after %d attempt(s). Last error: %s",
		name, opts.Timeout.Milliseconds(), attempt, lastMsg)
}

// Sleep waits for d or until ctx ends, whichever is first. It never returns
// an error: cancellation just ends the wait early. Mirrors TS `sleep`
// (d975097).
func Sleep(ctx context.Context, d time.Duration) {
	if ctx.Err() != nil || d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// NewConnectFunc returns a ConnectFunc for Channel that dials endpoint with
// opts on every (re)connect.
func NewConnectFunc(endpoint harness.PortEndpoint, opts DialOptions) ConnectFunc {
	return func(ctx context.Context) (Conn, error) { return Dial(ctx, endpoint, opts) }
}
