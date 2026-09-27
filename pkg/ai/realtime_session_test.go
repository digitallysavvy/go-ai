package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
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
