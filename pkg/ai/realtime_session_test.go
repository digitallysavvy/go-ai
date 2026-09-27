package ai

import (
	"context"
	"crypto/sha1" //nolint:gosec // required by the WebSocket handshake spec, not for security
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"golang.org/x/net/websocket"
)

type mockRealtimeModel struct{}

func (mockRealtimeModel) SpecificationVersion() string { return "v4" }
func (mockRealtimeModel) Provider() string             { return "mock.realtime" }
func (mockRealtimeModel) ModelID() string              { return "mock-model" }
func (mockRealtimeModel) DoCreateClientSecret(context.Context, provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	return provider.ClientSecretResult{Token: "t", URL: "ws://example.test"}, nil
}
func (mockRealtimeModel) GetWebSocketConfig(token, url string) provider.WebSocketConfig {
	return provider.WebSocketConfig{URL: url, Protocols: []string{"p." + token}}
}
func (mockRealtimeModel) BuildSessionConfig(config provider.RealtimeSessionConfig) any { return config }
func (mockRealtimeModel) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	var event map[string]interface{}
	_ = json.Unmarshal(raw, &event)
	return []provider.RealtimeServerEvent{{Type: event["type"].(string), Raw: raw}}, nil
}
func (mockRealtimeModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	return json.Marshal(map[string]interface{}{"type": event.Type, "audio": event.Audio})
}
func (mockRealtimeModel) GetHealthCheckResponse(raw json.RawMessage) (json.RawMessage, bool) {
	var event map[string]interface{}
	_ = json.Unmarshal(raw, &event)
	if event["type"] == "ping" {
		return json.RawMessage(`{"type":"pong"}`), true
	}
	return nil, false
}

type mockRealtimeDialer struct {
	conn   RealtimeWebSocketConn
	config provider.WebSocketConfig
}

func (d *mockRealtimeDialer) Dial(_ context.Context, config provider.WebSocketConfig) (RealtimeWebSocketConn, error) {
	d.config = config
	return d.conn, nil
}

type mockRealtimeConn struct {
	mu       sync.Mutex
	incoming [][]byte
	sent     [][]byte
	closed   bool
}

func (c *mockRealtimeConn) Send(_ context.Context, message []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, append([]byte(nil), message...))
	return nil
}

func (c *mockRealtimeConn) Receive(context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.incoming) == 0 {
		return nil, io.EOF
	}
	msg := c.incoming[0]
	c.incoming = c.incoming[1:]
	return msg, nil
}

func (c *mockRealtimeConn) Close() error {
	c.closed = true
	return nil
}

func TestRealtimeSessionConnectSendReadClose(t *testing.T) {
	conn := &mockRealtimeConn{incoming: [][]byte{[]byte(`not-json`), []byte(`{"type":"ping"}`), []byte(`{"type":"server-event"}`)}}
	dialer := &mockRealtimeDialer{conn: conn}
	instructions := "hello"
	session, err := ConnectRealtime(context.Background(), mockRealtimeModel{}, RealtimeSessionOptions{
		Dialer:        dialer,
		SessionConfig: &provider.RealtimeSessionConfig{Instructions: &instructions},
	})
	if err != nil {
		t.Fatalf("ConnectRealtime: %v", err)
	}
	if dialer.config.URL != "ws://example.test" || dialer.config.Protocols[0] != "p.t" {
		t.Fatalf("dial config = %+v", dialer.config)
	}
	if err := session.Send(context.Background(), provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "abc"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	events, err := session.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 1 || events[0].Type != "ping" {
		t.Fatalf("events = %+v", events)
	}
	if len(conn.sent) != 3 || string(conn.sent[0]) != `{"audio":"","type":"session-update"}` || string(conn.sent[2]) != `{"type":"pong"}` {
		t.Fatalf("sent = %q", conn.sent)
	}
	events, err = session.Read(context.Background())
	if err != nil {
		t.Fatalf("Read second: %v", err)
	}
	if len(events) != 1 || events[0].Type != "server-event" {
		t.Fatalf("second events = %+v", events)
	}
	if err := session.Close(); err != nil || !conn.closed {
		t.Fatalf("Close err=%v closed=%v", err, conn.closed)
	}
}

// rawRealtimeModel wraps mockRealtimeModel, additionally implementing
// provider.RealtimeRawEventSerializer and provider.RealtimeRawEventParser
// to exercise the raw string/binary client/server payload paths (audit row
// 8f89c25 / WG-MISC).
type rawRealtimeModel struct {
	mockRealtimeModel
}

func (rawRealtimeModel) SerializeClientEventRaw(event provider.RealtimeClientEvent) ([]byte, bool, bool, error) {
	if event.Type != "binary-audio" {
		return nil, false, false, nil // not handled: caller falls back to SerializeClientEvent.
	}
	return []byte(event.Audio), true, true, nil
}

func (rawRealtimeModel) ParseRawServerEvent(raw []byte, binary bool) ([]provider.RealtimeServerEvent, error) {
	if binary {
		return []provider.RealtimeServerEvent{{Type: "binary-frame", Raw: json.RawMessage(fmt.Sprintf("%q", raw))}}, nil
	}
	var event map[string]interface{}
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, nil // not valid JSON either: drop it, matching the non-raw-parser fallback.
	}
	typ, _ := event["type"].(string)
	return []provider.RealtimeServerEvent{{Type: typ, Raw: raw}}, nil
}

// mockBinaryRealtimeConn wraps mockRealtimeConn, additionally implementing
// RealtimeBinaryConn.
type mockBinaryRealtimeConn struct {
	*mockRealtimeConn
	incomingBinary []bool // parallel to incoming; true = binary frame.
	sentBinary     [][]byte
}

func (c *mockBinaryRealtimeConn) SendBinary(_ context.Context, message []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sentBinary = append(c.sentBinary, append([]byte(nil), message...))
	return nil
}

func (c *mockBinaryRealtimeConn) ReceiveFrame(ctx context.Context) ([]byte, bool, error) {
	binary := len(c.incomingBinary) > 0 && c.incomingBinary[0]
	if len(c.incomingBinary) > 0 {
		c.incomingBinary = c.incomingBinary[1:]
	}
	data, err := c.mockRealtimeConn.Receive(ctx)
	if err != nil {
		return nil, false, err
	}
	return data, binary, nil
}

// TestRealtimeSession_RawEventSerializerSendsBinaryFrame verifies that when
// the model implements RealtimeRawEventSerializer and opts in for an event,
// Send uses the raw payload directly (bypassing JSON serialization) and
// dispatches to RealtimeBinaryConn.SendBinary for a binary payload.
func TestRealtimeSession_RawEventSerializerSendsBinaryFrame(t *testing.T) {
	conn := &mockBinaryRealtimeConn{mockRealtimeConn: &mockRealtimeConn{}}
	dialer := &mockRealtimeDialer{conn: conn}
	session, err := ConnectRealtime(context.Background(), rawRealtimeModel{}, RealtimeSessionOptions{Dialer: dialer})
	if err != nil {
		t.Fatalf("ConnectRealtime: %v", err)
	}

	if err := session.Send(context.Background(), provider.RealtimeClientEvent{Type: "binary-audio", Audio: "raw-pcm-bytes"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(conn.sentBinary) != 1 || string(conn.sentBinary[0]) != "raw-pcm-bytes" {
		t.Fatalf("sentBinary = %q, want [\"raw-pcm-bytes\"]", conn.sentBinary)
	}
	if len(conn.sent) != 0 {
		t.Fatalf("expected no text sends, got %q", conn.sent)
	}

	// An event the model doesn't opt into falls back to normal JSON
	// serialization over the regular (text) Send path.
	if err := session.Send(context.Background(), provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "abc"}); err != nil {
		t.Fatalf("Send (fallback): %v", err)
	}
	if len(conn.sent) != 1 {
		t.Fatalf("expected the fallback event as a text send, got sent=%q sentBinary=%q", conn.sent, conn.sentBinary)
	}
}

// TestRealtimeSession_RawEventParserReceivesBinaryFrame verifies that when
// the model implements RealtimeRawEventParser, a binary frame (which would
// otherwise never be valid JSON and would be silently dropped) reaches
// ParseRawServerEvent instead of being discarded.
func TestRealtimeSession_RawEventParserReceivesBinaryFrame(t *testing.T) {
	conn := &mockBinaryRealtimeConn{
		mockRealtimeConn: &mockRealtimeConn{incoming: [][]byte{[]byte("\x00\x01\x02raw-audio-bytes")}},
		incomingBinary:   []bool{true},
	}
	dialer := &mockRealtimeDialer{conn: conn}
	session, err := ConnectRealtime(context.Background(), rawRealtimeModel{}, RealtimeSessionOptions{Dialer: dialer})
	if err != nil {
		t.Fatalf("ConnectRealtime: %v", err)
	}

	events, err := session.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 1 || events[0].Type != "binary-frame" {
		t.Fatalf("events = %+v, want a single binary-frame event", events)
	}
}

func TestGetRealtimeToolDefinitions(t *testing.T) {
	tools := []types.Tool{
		{Name: "lookup", Description: "Lookup data", Parameters: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}})},
		{Name: "no_description", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "native", Type: types.ToolTypeProviderDefined, Parameters: map[string]interface{}{"type": "object"}},
	}
	defs, err := GetRealtimeToolDefinitions(tools)
	if err != nil {
		t.Fatalf("GetRealtimeToolDefinitions: %v", err)
	}
	if len(defs) != 2 || defs[0].Name != "lookup" || defs[0].Type != "function" || defs[0].Parameters["type"] != "object" {
		t.Fatalf("defs = %+v", defs)
	}
	if defs[0].Description == nil || *defs[0].Description != "Lookup data" {
		t.Fatalf("description = %#v", defs[0].Description)
	}
	if defs[1].Description != nil {
		t.Fatalf("description should be omitted when absent, got %#v", defs[1].Description)
	}
}

// TestWebSocketRealtimeDialer_SendReceiveRoundTrip exercises the real
// golang.org/x/net/websocket-backed dialer (rather than the mock used by
// TestRealtimeSessionConnectSendReadClose) end-to-end: dial, send, receive,
// and a clean close reported as io.EOF.
func TestWebSocketRealtimeDialer_SendReceiveRoundTrip(t *testing.T) {
	wsHandler := websocket.Server{Handler: func(conn *websocket.Conn) {
		var msg string
		if err := websocket.Message.Receive(conn, &msg); err != nil {
			return
		}
		_ = websocket.Message.Send(conn, "echo:"+msg)
	}}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	dialer := WebSocketRealtimeDialer{}
	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := dialer.Dial(context.Background(), provider.WebSocketConfig{URL: wsURL})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	if err := conn.Send(context.Background(), []byte("hello")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got, err := conn.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if string(got) != "echo:hello" {
		t.Fatalf("Receive() = %q, want %q", got, "echo:hello")
	}
}

// TestWebSocketRealtimeDialer_CleanCloseIsEOF verifies a clean WebSocket
// close is reported as io.EOF, matching the RealtimeWebSocketConn contract
// RealtimeSession.Read relies on to end a session without an error.
func TestWebSocketRealtimeDialer_CleanCloseIsEOF(t *testing.T) {
	wsHandler := websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = conn.Close()
	}}
	ts := httptest.NewServer(wsHandler)
	defer ts.Close()

	dialer := WebSocketRealtimeDialer{}
	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := dialer.Dial(context.Background(), provider.WebSocketConfig{URL: wsURL})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.Receive(context.Background()); err != io.EOF {
		t.Fatalf("Receive() error = %v, want io.EOF for a clean close", err)
	}
}

// TestWebSocketRealtimeDialer_AbnormalDisconnectSurfacesError mirrors the
// Cartesia/xAI review's error-handling parity check: an abnormal
// disconnection (a connection dropped mid-frame via a TCP RST, distinct from
// a clean WebSocket close frame) must surface as a real error, not io.EOF.
func TestWebSocketRealtimeDialer_AbnormalDisconnectSurfacesError(t *testing.T) {
	ts := newRealtimeAbnormalDisconnectTestServer(t)
	defer ts.Close()

	dialer := WebSocketRealtimeDialer{}
	wsURL := "ws" + ts.URL[len("http"):]
	conn, err := dialer.Dial(context.Background(), provider.WebSocketConfig{URL: wsURL})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close() //nolint:errcheck

	_, err = conn.Receive(context.Background())
	if err == nil {
		t.Fatal("expected an error for an abnormal disconnection")
	}
	if err == io.EOF {
		t.Fatal("expected a real error, not io.EOF, for an abnormal disconnection")
	}
}

// newRealtimeAbnormalDisconnectTestServer performs the WebSocket handshake
// itself (rather than golang.org/x/net/websocket's server helper) so it can
// force a TCP RST (via SO_LINGER=0) instead of a clean FIN.
// golang.org/x/net/websocket parses frame headers one byte at a time via
// bufio.Reader.ReadByte, which returns a plain io.EOF for any ordinary
// closed/half-closed connection — indistinguishable, at that layer, from a
// properly received close frame. Only a genuine socket-level error (here,
// "connection reset by peer") is distinct from io.EOF.
func newRealtimeAbnormalDisconnectTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hijacker, ok := w.(stdhttp.Hijacker)
		if !ok {
			t.Fatalf("ResponseWriter does not support hijacking")
		}
		conn, buf, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}

		key := r.Header.Get("Sec-WebSocket-Key")
		accept := realtimeComputeWebSocketAccept(key)
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
	return httptest.NewServer(stdhttp.HandlerFunc(handler))
}

// realtimeComputeWebSocketAccept computes the Sec-WebSocket-Accept header
// value for a given Sec-WebSocket-Key, per RFC 6455 section 1.3.
func realtimeComputeWebSocketAccept(key string) string {
	const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New() //nolint:gosec // required by the WebSocket handshake spec, not for security
	h.Write([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
