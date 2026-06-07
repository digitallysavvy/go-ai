package ai

import (
	"context"
	"encoding/json"
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
	conn   *mockRealtimeConn
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
