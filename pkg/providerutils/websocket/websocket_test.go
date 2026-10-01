package websocket

import (
	"context"
	"crypto/sha1" //nolint:gosec // required by the WebSocket handshake spec, not for security
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestDial_SendsHeadersAndProtocols(t *testing.T) {
	gotHeaders := make(chan http.Header, 1)
	wsHandler := websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *http.Request) error {
			// AcceptHandshake requires exactly one protocol to be echoed back
			// when more than one was offered.
			if len(cfg.Protocol) > 0 {
				cfg.Protocol = cfg.Protocol[:1]
			}
			return nil
		},
		Handler: func(conn *websocket.Conn) {
			gotHeaders <- conn.Request().Header
			var msg string
			_ = websocket.Message.Receive(conn, &msg)
		},
	}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL, DialOptions{
		Headers:   map[string]string{"Authorization": "Bearer token", "X-Empty": ""},
		Protocols: []string{"proto-a", "proto-b"},
	})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	select {
	case h := <-gotHeaders:
		if got := h.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer token")
		}
		if h.Get("X-Empty") != "" {
			t.Fatalf("X-Empty header should be omitted when empty, got %q", h.Get("X-Empty"))
		}
		if got := h.Get("Sec-WebSocket-Protocol"); got != "proto-a, proto-b" {
			t.Fatalf("Sec-WebSocket-Protocol header = %q, want %q", got, "proto-a, proto-b")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to observe the handshake headers")
	}
}

// TestDial_FailureRedactsQueryStringFromError is a regression test: some
// providers (e.g. Cartesia's streaming transcription) place a bearer/access
// token directly in the WebSocket URL's query string. x/net/websocket's
// *websocket.DialError.Error() includes the full dial URL verbatim, so any
// dial failure (connection refused, DNS failure, TLS error, timeout) put the
// live token into the error returned to the caller. Dial must strip the
// query string before returning the error, while preserving the underlying
// cause (e.g. "connection refused").
func TestDial_FailureRedactsQueryStringFromError(t *testing.T) {
	const secret = "super-secret-token-redact-me"
	// Dialing a closed local port deterministically fails with "connection
	// refused" without needing a real network outage.
	wsURL := "ws://127.0.0.1:1/stt/turns/websocket?access_token=" + secret + "&model=ink-2"

	_, err := Dial(context.Background(), wsURL, DialOptions{})
	if err == nil {
		t.Fatal("expected a dial error, got nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("dial error leaked the access token: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected the underlying cause to be preserved, got: %v", err)
	}
}

func TestDial_UsesTargetOriginNotLocalhost(t *testing.T) {
	gotOrigin := make(chan string, 1)
	wsHandler := websocket.Server{Handler: func(conn *websocket.Conn) {
		gotOrigin <- conn.Request().Header.Get("Origin")
	}}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	select {
	case origin := <-gotOrigin:
		if origin == "http://localhost/" || origin == "" {
			t.Fatalf("Origin = %q, want the target's own origin, not a hardcoded localhost placeholder", origin)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to observe the Origin header")
	}
}

func TestSend_ReceiveRoundTrip(t *testing.T) {
	wsHandler := websocket.Server{Handler: func(conn *websocket.Conn) {
		var msg string
		if err := websocket.Message.Receive(conn, &msg); err != nil {
			return
		}
		_ = websocket.Message.Send(conn, "echo:"+msg)
	}}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	if err := Send(context.Background(), conn, "hello"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	out := make(chan Message, 1)
	go ReceiveLoop(context.Background(), conn, out)

	select {
	case msg := <-out:
		if msg.Err != nil {
			t.Fatalf("ReceiveLoop message error = %v", msg.Err)
		}
		if msg.Text != "echo:hello" {
			t.Fatalf("Text = %q, want %q", msg.Text, "echo:hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for echoed message")
	}
}

func TestReceive_OneShotRoundTrip(t *testing.T) {
	wsHandler := websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = websocket.Message.Send(conn, "hello")
	}}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	text, err := Receive(context.Background(), conn)
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if text != "hello" {
		t.Fatalf("text = %q, want %q", text, "hello")
	}
}

func TestReceive_UnblocksOnContextCancel(t *testing.T) {
	wsHandler := websocket.Server{Handler: func(conn *websocket.Conn) {
		<-context.Background().Done() // block forever without sending
	}}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Receive(ctx, conn); err == nil {
		t.Fatal("expected Receive to return an error for an already-cancelled context")
	}
}

func TestReceiveLoop_CleanCloseIsEOF(t *testing.T) {
	wsHandler := websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = conn.Close()
	}}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	out := make(chan Message, 1)
	go ReceiveLoop(context.Background(), conn, out)

	select {
	case msg := <-out:
		if !IsCleanClose(msg.Err) {
			t.Fatalf("IsCleanClose(%v) = false, want true for a clean server close", msg.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the close to be observed")
	}
}

// TestReceiveLoop_AbnormalDisconnectIsNotCleanClose mirrors the Cartesia/xAI
// regression coverage: an abnormal disconnection (TCP RST rather than a
// proper close frame) must be distinguishable from a clean close so callers
// can fail the stream instead of finishing it silently.
func TestReceiveLoop_AbnormalDisconnectIsNotCleanClose(t *testing.T) {
	ts := newAbnormalDisconnectTestServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	out := make(chan Message, 1)
	go ReceiveLoop(context.Background(), conn, out)

	select {
	case msg := <-out:
		if msg.Err == nil {
			t.Fatal("expected a receive error for an abnormal disconnection")
		}
		if IsCleanClose(msg.Err) {
			t.Fatalf("IsCleanClose(%v) = true, want false for an abnormal disconnection", msg.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the abnormal disconnect to be observed")
	}
}

func TestSend_UnblocksOnContextCancel(t *testing.T) {
	// A connection whose peer never reads leaves Send's underlying write
	// pending indefinitely once the socket's buffers fill; a cancelled ctx
	// must still return promptly.
	wsHandler := websocket.Server{Handler: func(conn *websocket.Conn) {
		<-context.Background().Done() // block forever without reading
	}}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := Dial(context.Background(), wsURL, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = Send(ctx, conn, "hello")
	if err == nil {
		t.Fatal("expected Send to return an error for an already-cancelled context")
	}
}

// newAbnormalDisconnectTestServer performs the WebSocket handshake itself
// (rather than golang.org/x/net/websocket's server helper) so it can force a
// TCP RST (via SO_LINGER=0) instead of a clean FIN. golang.org/x/net/websocket
// parses frame headers one byte at a time via bufio.Reader.ReadByte, which
// returns a plain io.EOF for any ordinary closed/half-closed connection —
// indistinguishable, at that layer, from a properly received close frame.
// Only a genuine socket-level error (here, "connection reset by peer") is
// distinct from io.EOF.
func newAbnormalDisconnectTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("ResponseWriter does not support hijacking")
		}
		conn, buf, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}

		key := r.Header.Get("Sec-WebSocket-Key")
		accept := computeWebSocketAccept(key)
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := buf.WriteString(resp); err != nil {
			conn.Close() //nolint:errcheck
			return
		}
		if err := buf.Flush(); err != nil {
			conn.Close() //nolint:errcheck
			return
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			// SetLinger(0) makes the following Close() send a RST instead of
			// the normal FIN/close handshake, forcing a real socket error on
			// the client's next read instead of a graceful io.EOF.
			_ = tcpConn.SetLinger(0)
		}
		conn.Close() //nolint:errcheck
	}
	return httptest.NewServer(http.HandlerFunc(handler))
}

// computeWebSocketAccept computes the Sec-WebSocket-Accept header value for a
// given Sec-WebSocket-Key, per RFC 6455 section 1.3.
func computeWebSocketAccept(key string) string {
	const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New() //nolint:gosec // required by the WebSocket handshake spec, not for security
	h.Write([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
