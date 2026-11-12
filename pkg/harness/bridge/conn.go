package bridge

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
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

// closeCodeUnauthorized is the close code the bridge uses for a connection
// that failed authentication (a wrong or missing proof; with a legacy bridge,
// a wrong or missing token).
const closeCodeUnauthorized = 1008

// UnauthorizedError means the bridge closed the connection with code 1008
// because it rejected the host's authentication proof (or, with a legacy
// bridge, its token). Retrying cannot help, so the retrying open fails fast
// on it. Mirrors TS `BridgeWebSocketAuthenticationError` for a 1008 close.
type UnauthorizedError struct{ Code int }

func (e *UnauthorizedError) Error() string {
	return fmt.Sprintf("The harness bridge rejected the WebSocket connection as unauthorized (code %d).", e.Code) //nolint:staticcheck // matches TS SDK's exact error text
}

// CloseError is returned by Conn.Receive when the socket closes. Code and
// Reason are the peer's close frame, as the TS `ws` close event reports them:
// 1005 for a close frame without a status code, 1006 for a connection that
// ended without a close frame or failed.
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
	closeInfo    *wsutil.CloseInfo // nil: close codes unknown
	writeTimeout time.Duration
	writeMu      sync.Mutex
	closeOnce    sync.Once
	closeErr     error
	// closed is set once the socket stops being open: Close was called, a
	// read failed (the peer closed it), or a write failed. Mirrors TS
	// `ws.readyState !== OPEN` for the channel's send gate.
	closed atomic.Bool
}

func (c *wsConn) isOpen() bool { return !c.closed.Load() }
func (c *wsConn) markClosed()  { c.closed.Store(true) }

// Receive reads one text or binary message.
func (c *wsConn) Receive() ([]byte, error) {
	var msg []byte
	if err := websocket.Message.Receive(c.ws, &msg); err != nil {
		c.closed.Store(true)
		if isEOF(err) {
			// x/net reports a received close frame (or an orderly TCP
			// shutdown) as io.EOF; closeInfo recovered the code and reason
			// like the TS `ws` close event (1005 without a status code,
			// 1006 without a close frame).
			if c.closeInfo != nil {
				return nil, &CloseError{Code: c.closeInfo.Code(), Reason: c.closeInfo.Reason(), Err: err}
			}
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

// CloseWithReason sends a close frame with code and reason, then closes the
// socket (TS `ws.close(code, reason)`). Safe to call more than once.
func (c *wsConn) CloseWithReason(code int, reason string) error {
	if !c.closed.Load() {
		c.writeMu.Lock()
		if c.writeTimeout > 0 {
			_ = c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout))
		}
		c.ws.PayloadType = websocket.CloseFrame
		_, _ = c.ws.Write(append([]byte{byte(code >> 8), byte(code)}, reason...))
		c.writeMu.Unlock()
	}
	return c.Close()
}

// reasonCloser is a Conn that can close with a close code and reason.
type reasonCloser interface {
	CloseWithReason(code int, reason string) error
}

// Close closes the socket. Safe to call more than once.
func (c *wsConn) Close() error {
	c.closed.Store(true)
	c.closeOnce.Do(func() { c.closeErr = c.ws.Close() })
	return c.closeErr
}

// DialOptions configures Dial.
type DialOptions struct {
	// Name labels the connection. Error texts follow TS ("The harness bridge
	// ..."), which does not include it; kept for compatibility.
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
	// AuthPrivateKey is the host-only BridgeAuthKeyPair.PrivateKey. When set,
	// Dial answers the bridge's authentication challenge (the first frame)
	// with a signed proof before anything else, also without WaitForHello,
	// and the hello deadline covers the challenge. A bridge that sends no
	// valid challenge fails with *AuthenticationError. Empty keeps the
	// legacy token flow (the token in the endpoint URL), which current
	// bridges reject.
	AuthPrivateKey string
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
	var authKey ed25519.PrivateKey
	if opts.AuthPrivateKey != "" {
		key, err := ParseBridgeAuthPrivateKey(opts.AuthPrivateKey)
		if err != nil {
			return nil, err
		}
		authKey = key
	}
	return dial(ctx, endpoint, opts, authKey)
}

func dial(ctx context.Context, endpoint harness.PortEndpoint, opts DialOptions, authKey ed25519.PrivateKey) (Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, abortError(ctx)
	}
	dialCtx := ctx
	if opts.OpenTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeoutCause(ctx, opts.OpenTimeout,
			fmt.Errorf("The harness bridge WebSocket did not open within %dms.", opts.OpenTimeout.Milliseconds())) //nolint:staticcheck // matches TS SDK's exact error text
		defer cancel()
	}
	// x/net/websocket reports every close as io.EOF; closeInfo reads the
	// server's close frame off the raw connection.
	closeInfo := &wsutil.CloseInfo{}
	ws, err := wsutil.Dial(dialCtx, endpoint.URL, wsutil.DialOptions{
		Headers:  endpoint.Headers,
		WrapConn: closeInfo.Wrap,
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, abortError(ctx)
		}
		if cause := context.Cause(dialCtx); cause != nil && dialCtx.Err() != nil {
			return nil, cause
		}
		return nil, sanitizeDialError(err)
	}
	writeTimeout := opts.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout
	}
	conn := &wsConn{ws: ws, closeInfo: closeInfo, writeTimeout: writeTimeout}
	authenticated := authKey == nil
	if !opts.WaitForHello && authenticated {
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
				return fail(fmt.Errorf("The harness bridge did not send bridge-hello within %dms.", helloTimeout.Milliseconds())) //nolint:staticcheck // matches TS SDK's exact error text
			}
			// Like the TS `ws` close event: the server's status code, 1005
			// for a close frame without one, 1006 for no close frame. Like
			// TS, 1008 is a rejected proof (or legacy token) and is final.
			code := closeInfo.Code()
			if code == closeCodeUnauthorized {
				return fail(&UnauthorizedError{Code: code})
			}
			return fail(fmt.Errorf("The harness bridge closed the WebSocket (code %d) before sending bridge-hello.", code)) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if !authenticated {
			// The first frame is the bridge's challenge: answer it before
			// anything else (TS openBridgeWebSocket onMessage).
			proof, err := CreateBridgeAuthProof(authKey, string(msg))
			if err != nil {
				return fail(&AuthenticationError{Message: invalidChallengeMessage})
			}
			authenticated = true
			if err := conn.Send([]byte(proof)); err != nil {
				return fail(err)
			}
			if !opts.WaitForHello {
				if !stop() {
					return fail(abortError(ctx))
				}
				_ = ws.SetReadDeadline(time.Time{})
				return conn, nil
			}
			continue
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

// parseHello returns the frame when it passes the TS hello schema
// (harnessV1BridgeHelloSchema): a `bridge-hello` object whose optional keys
// are not null and have the right types. Other frames, and hellos that fail
// the schema, are ignored, as in TS.
func parseHello(msg []byte) (*Hello, bool) {
	frame, _, err := DecodeOutbound(msg)
	if err != nil {
		return nil, false
	}
	hello, ok := frame.(*Hello)
	return hello, ok
}

// SupportsUserMessageResponses reports the hello capability
// `experimental_userMessageResponses === true`.
func (h *Hello) SupportsUserMessageResponses() bool {
	return h != nil && h.Capabilities != nil && h.Capabilities.ExperimentalUserMessageResponses != nil &&
		*h.Capabilities.ExperimentalUserMessageResponses
}

// SupportsIdempotentStart reports the hello capability
// `idempotentStart === true`.
func (h *Hello) SupportsIdempotentStart() bool {
	return h != nil && h.Capabilities != nil && h.Capabilities.IdempotentStart != nil &&
		*h.Capabilities.IdempotentStart
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

// sanitizeDialError strips the dial target's query string out of a
// golang.org/x/net/websocket *DialError before it is allowed to propagate.
// A legacy (token) endpoint carries the bridge's secret channel token in the
// dial URL's query string via WithBridgeToken (the `agent_bridge_token`
// param), and a caller-supplied endpoint URL may carry other secrets. x/net's
// DialError.Error() unconditionally renders the full dial URL
// (config.Location.String()) for every kind of dial failure — DNS failure,
// connection refused, TLS failure, a non-101 handshake response — so without
// this, that token would leak into whatever this error's message reaches:
// Channel.Open's returned error, OpenBridgeWebSocket's composed "last error"
// message, and ChannelDebugEvent.Cause on a reconnect-failed event, any of
// which calling code may log or display. Any other error shape (e.g. the
// ctx-derived errors already handled above) is returned unchanged.
func sanitizeDialError(err error) error {
	var dialErr *websocket.DialError
	if !errors.As(err, &dialErr) || dialErr.Config == nil || dialErr.Location == nil {
		return err
	}
	redactedURL := *dialErr.Location
	if redactedURL.RawQuery != "" {
		redactedURL.RawQuery = "REDACTED"
	}
	return fmt.Errorf("websocket.Dial %s: %w", redactedURL.String(), dialErr.Err)
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
	// AuthPrivateKey answers the bridge's authentication challenge on every
	// attempt (see DialOptions.AuthPrivateKey). An invalid key fails before
	// any attempt.
	AuthPrivateKey string
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
	var authKey ed25519.PrivateKey
	if opts.AuthPrivateKey != "" {
		key, err := ParseBridgeAuthPrivateKey(opts.AuthPrivateKey)
		if err != nil {
			return nil, err
		}
		authKey = key
	}
	return openBridgeWebSocket(ctx, endpoint, opts, name, authKey)
}

func openBridgeWebSocket(ctx context.Context, endpoint harness.PortEndpoint, opts OpenOptions, name string, authKey ed25519.PrivateKey) (Conn, error) {
	deadline := time.Now().Add(opts.Timeout)
	attempt := 0
	var lastErr error
	for ctx.Err() == nil && time.Now().Before(deadline) {
		attempt++
		remaining := max(time.Until(deadline), time.Millisecond)
		conn, err := dial(ctx, endpoint, DialOptions{
			Name:          name,
			OpenTimeout:   min(10*time.Second, remaining),
			WaitForHello:  true,
			HelloTimeout:  5 * time.Second,
			HelloDeadline: deadline,
			OnHello:       opts.OnHello,
			WriteTimeout:  opts.WriteTimeout,
		}, authKey)
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, abortError(ctx)
		}
		var unauthorized *UnauthorizedError
		var authErr *AuthenticationError
		if errors.As(err, &unauthorized) || errors.As(err, &authErr) {
			return nil, err
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
	// Like TS, the message omits the last error (it can carry connection
	// details); it is the error's cause.
	return nil, harness.NewHarnessError(
		fmt.Sprintf("The harness bridge did not complete the WebSocket handshake within %dms after %d attempt(s).",
			opts.Timeout.Milliseconds(), attempt), lastErr)
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
