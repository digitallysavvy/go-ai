package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lifecycleTransport answers initialize with the configured result and, for
// every other method, calls onRequest. Receive blocks on in until the test
// closes it.
type lifecycleTransport struct {
	in        chan *MCPMessage
	initial   InitializeResult
	failInit  bool
	onRequest func(msg *MCPMessage)

	mu      sync.Mutex
	sent    []string
	closed  int32
	closeCh chan struct{}
	once    sync.Once
}

func newLifecycleTransport(caps ServerCapabilities) *lifecycleTransport {
	return &lifecycleTransport{
		in:      make(chan *MCPMessage, 10),
		closeCh: make(chan struct{}),
		initial: InitializeResult{ProtocolVersion: ProtocolVersion, Capabilities: caps, ServerInfo: ServerInfo{Name: "s", Version: "1"}},
	}
}

func (t *lifecycleTransport) Connect(context.Context) error { return nil }
func (t *lifecycleTransport) IsConnected() bool             { return atomic.LoadInt32(&t.closed) == 0 }
func (t *lifecycleTransport) Close() error {
	atomic.StoreInt32(&t.closed, 1)
	t.once.Do(func() { close(t.closeCh) })
	return nil
}
func (t *lifecycleTransport) Receive(ctx context.Context) (*MCPMessage, error) {
	select {
	case m, ok := <-t.in:
		if !ok {
			return nil, io.EOF
		}
		return m, nil
	case <-t.closeCh:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (t *lifecycleTransport) Send(_ context.Context, msg *MCPMessage) error {
	t.mu.Lock()
	t.sent = append(t.sent, msg.Method)
	t.mu.Unlock()
	if msg.Method == "initialize" {
		if t.failInit {
			resp := &MCPMessage{JSONRpc: "2.0", ID: msg.ID, Error: &MCPError{Code: -32000, Message: "nope"}}
			t.in <- resp
			return nil
		}
		data, _ := json.Marshal(t.initial)
		t.in <- &MCPMessage{JSONRpc: "2.0", ID: msg.ID, Result: data}
		return nil
	}
	if t.onRequest != nil && msg.ID != nil {
		t.onRequest(msg)
	}
	return nil
}
func (t *lifecycleTransport) methods() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.sent...)
}

// TS assertCapability: requests for features the server did not advertise
// fail before anything is sent.
func TestMCPClientRejectsUnadvertisedCapabilities(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{})
	client := NewMCPClient(tr, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck
	ctx := context.Background()
	checks := map[string]func() error{
		"tools":     func() error { _, err := client.ListTools(ctx); return err },
		"tools ":    func() error { _, err := client.CallTool(ctx, "x", nil); return err },
		"resources": func() error { _, err := client.ListResources(ctx); return err },
		"prompts":   func() error { _, err := client.ListPrompts(ctx); return err },
	}
	for want, fn := range checks {
		err := fn()
		want = strings.TrimSpace(want)
		var mcpErr *MCPClientError
		if !errors.As(err, &mcpErr) || mcpErr.Message != "Server does not support "+want {
			t.Fatalf("%s: err = %v", want, err)
		}
	}
	for _, m := range tr.methods() {
		if m != "initialize" && m != "notifications/initialized" {
			t.Fatalf("unexpected request sent: %s", m)
		}
	}
}

// TS onClose rejects pending requests with "Connection closed" right away.
func TestMCPClientFailsPendingRequestsWhenTransportCloses(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{Tools: &ToolsCapability{}})
	client := NewMCPClient(tr, MCPClientConfig{RequestTimeoutMS: 20000})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck
	tr.onRequest = func(*MCPMessage) { close(tr.in) } // server dies instead of answering

	done := make(chan error, 1)
	go func() { _, err := client.ListTools(context.Background()); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "Connection closed") {
			t.Fatalf("err = %v, want Connection closed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending request was not failed when the transport closed")
	}
	if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "closed client") {
		t.Fatalf("later request err = %v, want closed client error", err)
	}
}

// TS init() closes the transport when the handshake fails.
func TestMCPClientConnectFailureClosesTransport(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{})
	tr.failInit = true
	client := NewMCPClient(tr, MCPClientConfig{})
	if err := client.Connect(context.Background()); err == nil {
		t.Fatal("expected Connect to fail")
	}
	if atomic.LoadInt32(&tr.closed) != 1 {
		t.Fatal("transport was not closed after a failed handshake")
	}
}

// TS initialInitializeResult skips the initialize handshake.
func TestMCPClientReusesInitialInitializeResult(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{})
	initial := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ServerCapabilities{Tools: &ToolsCapability{}},
		ServerInfo:      ServerInfo{Name: "cached", Version: "9"},
		Instructions:    "cached instructions",
	}
	client := NewMCPClient(tr, MCPClientConfig{InitialInitializeResult: &initial})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck
	if len(tr.methods()) != 0 {
		t.Fatalf("sent %v, want nothing", tr.methods())
	}
	if client.ServerInfo().Name != "cached" || client.ServerInstructions() != "cached instructions" || client.ServerCapabilities().Tools == nil {
		t.Fatalf("cached result not applied: %+v", client.InitializeResult())
	}

	bad := InitializeResult{ProtocolVersion: "1999-01-01"}
	if err := NewMCPClient(newLifecycleTransport(ServerCapabilities{}), MCPClientConfig{InitialInitializeResult: &bad}).Connect(context.Background()); err == nil {
		t.Fatal("unsupported protocol version should fail")
	}
}
