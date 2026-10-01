// Package websocket is the Go equivalent of the TypeScript AI SDK's
// connectToWebSocket helper (packages/provider-utils/src/connect-to-websocket.ts):
// a small, transport-generic layer over golang.org/x/net/websocket that every
// WebSocket-backed provider stream (streaming transcription, realtime
// sessions) builds on, instead of each provider re-implementing dial/send/
// receive plumbing.
//
// Unlike the JS WebSocket API (event callbacks), Go providers drive a
// goroutine + channel loop, so this package exposes the equivalent primitives
// directly rather than an event-driven connection object:
//
//   - Dial opens the connection (DialContext, headers, subprotocols, and the
//     Origin handshake header x/net/websocket requires).
//   - ReceiveLoop continuously reads text frames into a channel until an
//     error occurs or ctx is done, mirroring onMessageText/onSocketError/
//     onClose.
//   - Send writes a frame, honoring ctx cancellation.
//   - IsCleanClose distinguishes a clean WebSocket close (TS onClose) from an
//     abnormal disconnection (TS onSocketError): golang.org/x/net/websocket
//     surfaces both as a plain error from Message.Receive, with a clean close
//     reported as io.EOF.
package websocket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"golang.org/x/net/websocket"
)

// DialOptions configures Dial. Both fields are optional.
type DialOptions struct {
	// Headers are additional headers sent with the WebSocket opening
	// handshake (e.g. Authorization, x-api-key). Empty values are omitted,
	// mirroring TS connectToWebSocket's removeUndefinedEntries.
	Headers map[string]string

	// Protocols are the WebSocket subprotocols offered in the handshake
	// (Sec-WebSocket-Protocol), mirroring TS's `protocols` option.
	Protocols []string
}

// Dial opens a client WebSocket connection to rawURL, mirroring TS
// connectToWebSocket's constructor step: DialContext (rather than
// websocket.Dial/DialConfig, which always dial against
// context.Background()) so a cancelled ctx fails a pending handshake and
// cleans up the socket, instead of leaving an unread, unclosed connection
// behind if the dial completes after the caller has already given up on it.
//
// golang.org/x/net/websocket requires an Origin handshake header (unlike a
// browser or Node WebSocket client, which TS's connectToWebSocket never sets
// itself). Absent a documented origin for these server-to-server provider
// connections, the target URL's own origin is used — a same-origin handshake
// header rather than the historical hardcoded "http://localhost/" every
// caller used to duplicate.
func Dial(ctx context.Context, rawURL string, opts DialOptions) (*websocket.Conn, error) {
	origin, err := deriveOrigin(rawURL)
	if err != nil {
		return nil, err
	}

	wsConfig, err := websocket.NewConfig(rawURL, origin)
	if err != nil {
		return nil, err
	}
	if len(opts.Protocols) > 0 {
		wsConfig.Protocol = opts.Protocols
	}
	if len(opts.Headers) > 0 {
		wsConfig.Header = http.Header{}
		for k, v := range opts.Headers {
			if v != "" {
				wsConfig.Header.Set(k, v)
			}
		}
	}

	conn, err := wsConfig.DialContext(ctx)
	if err != nil {
		return nil, redactDialError(rawURL, err)
	}
	return conn, nil
}

// redactDialError strips query-string parameters from rawURL before folding
// it into a dial failure's error message. Some providers (e.g. Cartesia's
// streaming transcription) place a bearer/access token directly in the
// WebSocket URL's query string, since the handshake has no header-based
// alternative; x/net/websocket's *websocket.DialError.Error() includes the
// full dial URL verbatim (query string and all), so an unredacted dial
// error would put a live token into an error that calling code commonly
// logs or surfaces. The underlying cause (e.g. "dial tcp ...: connection
// refused") is preserved via %w.
func redactDialError(rawURL string, err error) error {
	redacted := redactURLQuery(rawURL)
	var dialErr *websocket.DialError
	if errors.As(err, &dialErr) && dialErr.Err != nil {
		return fmt.Errorf("websocket dial %s: %w", redacted, dialErr.Err)
	}
	return fmt.Errorf("websocket dial %s: %w", redacted, err)
}

// redactURLQuery returns rawURL with its query string removed. Falls back to
// returning rawURL unchanged if it fails to parse (defense in depth only --
// Dial itself already parses rawURL successfully before this is reached).
func redactURLQuery(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.RawQuery = ""
	return u.String()
}

// deriveOrigin returns the http(s) origin (scheme + host, root path) for a
// ws(s) target URL, e.g. "wss://api.example.com/v1/stream?x=1" ->
// "https://api.example.com/".
func deriveOrigin(rawURL string) (string, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	origin := *target
	switch origin.Scheme {
	case "wss":
		origin.Scheme = "https"
	case "ws":
		origin.Scheme = "http"
	}
	origin.Path = "/"
	origin.RawQuery = ""
	origin.Fragment = ""
	return origin.String(), nil
}

// Message is one received text frame, or the terminal error that ended the
// receive loop.
type Message struct {
	Text string
	Err  error
}

// ReceiveLoop continuously reads text frames from conn and forwards each one
// (or the terminal error) on out, until a read error occurs or ctx is done.
// It mirrors TS connectToWebSocket's onmessage/onerror/onclose wiring, run on
// a dedicated goroutine since Go has no event-driven WebSocket API to hang
// callbacks off of.
func ReceiveLoop(ctx context.Context, conn *websocket.Conn, out chan<- Message) {
	for {
		var msg string
		err := websocket.Message.Receive(conn, &msg)
		select {
		case out <- Message{Text: msg, Err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

// Receive reads a single text frame from conn, honoring ctx cancellation.
// Callers that pump messages continuously should prefer ReceiveLoop, run on
// its own goroutine; Receive is for callers that pull one message per call
// (e.g. a caller-driven duplex session rather than a push-based stream).
func Receive(ctx context.Context, conn *websocket.Conn) (string, error) {
	done := make(chan Message, 1)
	go func() {
		var msg string
		err := websocket.Message.Receive(conn, &msg)
		done <- Message{Text: msg, Err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-done:
		return res.Text, res.Err
	}
}

// Send writes v (a string for a text frame, or []byte for a binary frame) to
// conn, mirroring golang.org/x/net/websocket.Message's type-based framing so
// callers get the same wire behavior as TS's `socket.send(value)`. The write
// runs on its own goroutine so a cancelled ctx unblocks the caller even if
// the underlying write is hung.
func Send(ctx context.Context, conn *websocket.Conn, v interface{}) error {
	done := make(chan error, 1)
	go func() {
		done <- websocket.Message.Send(conn, v)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// IsCleanClose reports whether err represents a clean WebSocket close (TS
// connectToWebSocket's onClose) rather than an abnormal disconnection (TS's
// onSocketError). golang.org/x/net/websocket surfaces both as a plain error
// from Message.Receive with no close-code information to key on; a clean
// close is reported as io.EOF, so any other non-nil error is a socket-level
// error that must fail the stream rather than finish it silently.
func IsCleanClose(err error) bool {
	return errors.Is(err, io.EOF)
}
