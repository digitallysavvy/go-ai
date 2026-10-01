package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// headerCapableTransport is a modern-era-capable Transport that also
// implements MCPToolParameterHeadersTransport and HeaderedSendTransport,
// used to exercise the full x-mcp-header wiring end to end (hash 0c60a40):
// MCPClient.ListTools computing/caching bindings from a tool's input schema,
// and MCPClient.CallTool deriving and attaching `Mcp-Param-*` headers via
// SendWithHeaders.
type headerCapableTransport struct {
	mu        sync.Mutex
	messages  chan *MCPMessage
	connected bool

	tools []MCPTool

	lastCallHeaders       map[string]string
	sendWithHeadersCalled bool
	plainSendCalled       bool
}

func newHeaderCapableTransport(tools []MCPTool) *headerCapableTransport {
	return &headerCapableTransport{messages: make(chan *MCPMessage, 10), tools: tools}
}

func (t *headerCapableTransport) Connect(ctx context.Context) error { t.connected = true; return nil }
func (t *headerCapableTransport) Close() error {
	t.connected = false
	if t.messages != nil {
		close(t.messages)
	}
	return nil
}
func (t *headerCapableTransport) IsConnected() bool                      { return t.connected }
func (t *headerCapableTransport) SetProtocolVersion(_ string)            {}
func (t *headerCapableTransport) SupportsProtocolVersionDiscovery() bool { return true }
func (t *headerCapableTransport) SupportsMCPToolParameterHeaders() bool  { return true }

func (t *headerCapableTransport) respond(id interface{}, result interface{}) {
	response := &MCPMessage{JSONRpc: "2.0", ID: id}
	data, _ := json.Marshal(result)
	response.Result = data
	select {
	case t.messages <- response:
	default:
	}
}

func (t *headerCapableTransport) Send(ctx context.Context, msg *MCPMessage) error {
	t.mu.Lock()
	t.plainSendCalled = true
	t.mu.Unlock()
	return t.handle(msg)
}

func (t *headerCapableTransport) SendWithHeaders(ctx context.Context, msg *MCPMessage, headers map[string]string) error {
	t.mu.Lock()
	t.sendWithHeadersCalled = true
	t.lastCallHeaders = headers
	t.mu.Unlock()
	return t.handle(msg)
}

func (t *headerCapableTransport) handle(msg *MCPMessage) error {
	switch msg.Method {
	case "server/discover":
		t.respond(msg.ID, DiscoverResult{
			SupportedVersions: []string{LatestProtocolVersion},
			Capabilities:      ServerCapabilities{Tools: &ToolsCapability{}},
			ResultType:        "success",
		})
	case "tools/list":
		t.respond(msg.ID, map[string]interface{}{"tools": t.tools, "resultType": "success"})
	case "tools/call":
		t.respond(msg.ID, map[string]interface{}{
			"content":    []map[string]interface{}{{"type": "text", "text": "ok"}},
			"resultType": "success",
		})
	}
	return nil
}

func (t *headerCapableTransport) Receive(ctx context.Context) (*MCPMessage, error) {
	select {
	case msg, ok := <-t.messages:
		if !ok {
			return nil, context.Canceled
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestMCPClientToolCallSendsXMCPHeaderDerivedHeaders is the end-to-end test
// for hash 0c60a40: a tool's `x-mcp-header`-annotated input schema property
// becomes an HTTP header on its tools/call request.
func TestMCPClientToolCallSendsXMCPHeaderDerivedHeaders(t *testing.T) {
	tool := MCPTool{
		Name: "search",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"region": map[string]interface{}{"type": "string", "x-mcp-header": "Region"},
				"query":  map[string]interface{}{"type": "string"},
			},
		},
	}
	transport := newHeaderCapableTransport([]MCPTool{tool})
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Fatalf("tools = %#v", tools)
	}

	result, err := client.CallTool(context.Background(), "search", map[string]interface{}{
		"region": "us-west",
		"query":  "widgets",
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("result = %#v", result)
	}

	transport.mu.Lock()
	defer transport.mu.Unlock()
	if !transport.sendWithHeadersCalled {
		t.Fatal("expected tools/call to be sent via SendWithHeaders")
	}
	if transport.lastCallHeaders["Mcp-Param-Region"] != "us-west" {
		t.Fatalf("headers = %#v, want Mcp-Param-Region=us-west", transport.lastCallHeaders)
	}
}

// TestMCPClientToolCallWithoutHeaderBindingsUsesPlainSend verifies a tool
// with no x-mcp-header annotations still calls the plain Send path (no
// headers to attach).
func TestMCPClientToolCallWithoutHeaderBindingsUsesPlainSend(t *testing.T) {
	tool := MCPTool{
		Name:        "no-headers",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
	transport := newHeaderCapableTransport([]MCPTool{tool})
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if _, err := client.CallTool(context.Background(), "no-headers", nil); err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.sendWithHeadersCalled {
		t.Fatal("expected plain Send (no header bindings), not SendWithHeaders")
	}
	if !transport.plainSendCalled {
		t.Fatal("expected Send to have been called")
	}
}

// TestMCPClientFiltersOutToolsWithInvalidHeaderBindings mirrors TS
// prepareToolDefinitions dropping a tool whose x-mcp-header annotation is
// invalid, reporting it via MCPClientConfig.OnError.
func TestMCPClientFiltersOutToolsWithInvalidHeaderBindings(t *testing.T) {
	badTool := MCPTool{
		Name: "bad-tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"count": map[string]interface{}{"type": "number", "x-mcp-header": "Count"},
			},
		},
	}
	goodTool := MCPTool{
		Name:        "good-tool",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
	transport := newHeaderCapableTransport([]MCPTool{badTool, goodTool})
	var reported []error
	client := NewMCPClient(transport, MCPClientConfig{OnError: func(err error) { reported = append(reported, err) }})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "good-tool" {
		t.Fatalf("tools = %#v, want only good-tool", tools)
	}
	if len(reported) != 1 {
		t.Fatalf("OnError calls = %d, want 1", len(reported))
	}
}
