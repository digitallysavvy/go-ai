package bridge

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// ConnectReason says why a Channel is connecting. Mirrors TS
// `SandboxChannelConnectOptions.reason`.
type ConnectReason string

const (
	// ConnectReasonOpen is the initial Channel.Open call.
	ConnectReasonOpen ConnectReason = "open"
	// ConnectReasonReconnect is an attempt to recover from a transient
	// disconnect. The channel retries those itself.
	ConnectReasonReconnect ConnectReason = "reconnect"
)

type connectReasonKey struct{}

// WithConnectReason returns ctx tagged with the reason a ConnectFunc is
// called. Channel tags every connect context; the ConnectFunc signature stays
// unchanged so existing callers keep compiling.
func WithConnectReason(ctx context.Context, reason ConnectReason) context.Context {
	return context.WithValue(ctx, connectReasonKey{}, reason)
}

// ConnectReasonFromContext returns the reason a ConnectFunc was called, or
// ConnectReasonOpen for a context a Channel did not tag.
func ConnectReasonFromContext(ctx context.Context) ConnectReason {
	if reason, ok := ctx.Value(connectReasonKey{}).(ConnectReason); ok {
		return reason
	}
	return ConnectReasonOpen
}

// BridgeConnectOptions configures NewBridgeConnectFunc.
type BridgeConnectOptions struct {
	// Name labels error messages. Default "bridge".
	Name string
	// OpenRetryTimeout is the retry budget for the channel's initial open
	// (ConnectReasonOpen). Zero makes a single attempt. Reconnects always make
	// a single attempt, because the channel retries them itself.
	OpenRetryTimeout time.Duration
	// StartContext additionally cancels the channel's initial open, e.g. the
	// context of the harness start call. It is not applied to reconnects.
	StartContext context.Context
	// OnHello is called with the `bridge-hello` frame after every successful
	// connect.
	OnHello func(*Hello)
	// WriteTimeout bounds each write (default DefaultWriteTimeout).
	WriteTimeout time.Duration
}

const (
	bridgeOpenTimeout  = 10 * time.Second
	bridgeHelloTimeout = 5 * time.Second
)

// NewBridgeConnectFunc is the shared connect function for bridge-backed
// adapters. Mirrors TS `createBridgeWebSocketConnect`.
//
// A connection only counts as established once the socket is open and the
// bridge has sent `bridge-hello`. Some sandbox runtimes complete the host-side
// upgrade before the connection reaches the bridge process, and frames sent in
// that gap are dropped. The bridge sends `bridge-hello` the instant it accepts a
// connection and nothing else until the host sends `start` or `resume`, so no
// frame is lost between the handshake and the channel wiring the socket.
//
// A connection the bridge closes with code 1008 (bad token) fails at once with
// *UnauthorizedError instead of being retried. x/net/websocket does not expose
// close codes, so Dial reads the code off the raw connection (websocket.CloseInfo).
func NewBridgeConnectFunc(endpoint harness.PortEndpoint, opts BridgeConnectOptions) ConnectFunc {
	name := opts.Name
	if name == "" {
		name = "bridge"
	}
	return func(ctx context.Context) (Conn, error) {
		initial := ConnectReasonFromContext(ctx) == ConnectReasonOpen
		if initial && opts.StartContext != nil {
			var cancel context.CancelCauseFunc
			ctx, cancel = context.WithCancelCause(ctx)
			defer cancel(nil)
			stop := context.AfterFunc(opts.StartContext, func() { cancel(abortError(opts.StartContext)) })
			defer stop()
		}
		if initial && opts.OpenRetryTimeout > 0 {
			return OpenBridgeWebSocket(ctx, endpoint, OpenOptions{
				Name: name, Timeout: opts.OpenRetryTimeout, OnHello: opts.OnHello, WriteTimeout: opts.WriteTimeout,
			})
		}
		return Dial(ctx, endpoint, DialOptions{
			Name: name, OpenTimeout: bridgeOpenTimeout, WaitForHello: true, HelloTimeout: bridgeHelloTimeout,
			OnHello: opts.OnHello, WriteTimeout: opts.WriteTimeout,
		})
	}
}
