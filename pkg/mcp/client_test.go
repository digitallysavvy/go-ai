package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"testing"
	"time"
)

// mockTransport implements Transport interface for testing
type mockTransport struct {
	messages         chan *MCPMessage
	connected        bool
	protocolVersion  string
	initializeParams InitializeParams
	sentMu           sync.Mutex
	sent             []*MCPMessage
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		messages: make(chan *MCPMessage, 10),
	}
}

func TestMCPRequestParamsMarshalArgumentsLikeTypeScript(t *testing.T) {
	toolNil, err := json.Marshal(CallToolParams{Name: "lookup"})
	if err != nil {
		t.Fatalf("marshal nil tool params: %v", err)
	}
	if string(toolNil) != `{"name":"lookup","arguments":{}}` {
		t.Fatalf("nil tool params JSON = %s, want arguments empty object", toolNil)
	}

	toolEmpty, err := json.Marshal(CallToolParams{Name: "lookup", Arguments: map[string]interface{}{}})
	if err != nil {
		t.Fatalf("marshal empty tool params: %v", err)
	}
	if string(toolEmpty) != `{"name":"lookup","arguments":{}}` {
		t.Fatalf("empty tool params JSON = %s, want arguments empty object", toolEmpty)
	}

	promptNil, err := json.Marshal(GetPromptParams{Name: "review"})
	if err != nil {
		t.Fatalf("marshal nil prompt params: %v", err)
	}
	if string(promptNil) != `{"name":"review"}` {
		t.Fatalf("nil prompt params JSON = %s, want arguments omitted", promptNil)
	}

	promptEmpty, err := json.Marshal(GetPromptParams{Name: "review", Arguments: map[string]interface{}{}})
	if err != nil {
		t.Fatalf("marshal empty prompt params: %v", err)
	}
	if string(promptEmpty) != `{"name":"review","arguments":{}}` {
		t.Fatalf("empty prompt params JSON = %s, want arguments empty object", promptEmpty)
	}
}

func TestMCPCallToolResultMarshalIsErrorDefaultLikeTypeScript(t *testing.T) {
	var contentResult CallToolResult
	if err := json.Unmarshal([]byte(`{"content":[{"type":"text","text":"ok"}]}`), &contentResult); err != nil {
		t.Fatalf("unmarshal content result: %v", err)
	}
	contentJSON, err := json.Marshal(contentResult)
	if err != nil {
		t.Fatalf("marshal content result: %v", err)
	}
	if string(contentJSON) != `{"content":[{"type":"text","text":"ok"}],"isError":false}` {
		t.Fatalf("content result JSON = %s, want TS default isError false", contentJSON)
	}

	manualContentJSON, err := json.Marshal(CallToolResult{
		Content: []ToolResultContent{{Type: "text", Text: "ok"}},
	})
	if err != nil {
		t.Fatalf("marshal manual content result: %v", err)
	}
	if string(manualContentJSON) != `{"content":[{"type":"text","text":"ok"}],"isError":false}` {
		t.Fatalf("manual content result JSON = %s, want TS default isError false", manualContentJSON)
	}

	var legacyResult CallToolResult
	if err := json.Unmarshal([]byte(`{"toolResult":{"ok":true}}`), &legacyResult); err != nil {
		t.Fatalf("unmarshal legacy result: %v", err)
	}
	legacyJSON, err := json.Marshal(legacyResult)
	if err != nil {
		t.Fatalf("marshal legacy result: %v", err)
	}
	if string(legacyJSON) != `{"toolResult":{"ok":true}}` {
		t.Fatalf("legacy result JSON = %s, want no default isError", legacyJSON)
	}

	var looseContentResult CallToolResult
	if err := json.Unmarshal([]byte(`{"content":[],"structuredContent":null,"extra":{"kept":true}}`), &looseContentResult); err != nil {
		t.Fatalf("unmarshal loose content result: %v", err)
	}
	looseJSON, err := json.Marshal(looseContentResult)
	if err != nil {
		t.Fatalf("marshal loose content result: %v", err)
	}
	if string(looseJSON) != `{"content":[],"structuredContent":null,"isError":false,"extra":{"kept":true}}` {
		t.Fatalf("loose content result JSON = %s, want TS loose-object fields and isError default", looseJSON)
	}
}

func (m *mockTransport) Connect(ctx context.Context) error {
	m.connected = true
	return nil
}

func (m *mockTransport) Close() error {
	m.connected = false
	if m.messages != nil {
		close(m.messages)
	}
	return nil
}

func (m *mockTransport) IsConnected() bool {
	return m.connected
}

func (m *mockTransport) SetProtocolVersion(version string) {
	m.protocolVersion = version
}

func (m *mockTransport) Send(ctx context.Context, msg *MCPMessage) error {
	m.sentMu.Lock()
	m.sent = append(m.sent, msg)
	m.sentMu.Unlock()

	// Simulate server response for tools/list
	if msg.Method == "tools/list" {
		response := &MCPMessage{
			JSONRpc: "2.0",
			ID:      msg.ID,
		}

		result := ListToolsResult{
			Tools: []MCPTool{
				{
					Name:        "test-tool",
					Description: "A test tool",
					InputSchema: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"input": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
			NextCursor: "next-page-cursor",
		}

		resultBytes, _ := json.Marshal(result)
		response.Result = resultBytes

		select {
		case m.messages <- response:
		default:
		}
	}

	if msg.Method == "tools/call" {
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := CallToolResult{
			Content: []ToolResultContent{
				{Type: "text", Text: "tool-ok"},
			},
		}
		resultBytes, _ := json.Marshal(result)
		response.Result = resultBytes
		select {
		case m.messages <- response:
		default:
		}
	}

	if msg.Method == "resources/list" {
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := ListResourcesResult{
			Resources: []MCPResource{
				{Name: "r1", URI: "file://r1.txt"},
			},
		}
		resultBytes, _ := json.Marshal(result)
		response.Result = resultBytes
		select {
		case m.messages <- response:
		default:
		}
	}

	if msg.Method == "resources/read" {
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := ReadResourceResult{
			Contents: []ResourceContent{
				{URI: "file://r1.txt", Text: "hello"},
			},
		}
		resultBytes, _ := json.Marshal(result)
		response.Result = resultBytes
		select {
		case m.messages <- response:
		default:
		}
	}

	if msg.Method == "prompts/list" {
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := ListPromptsResult{
			Prompts: []MCPPrompt{
				{Name: "p1", Description: "prompt one"},
			},
		}
		resultBytes, _ := json.Marshal(result)
		response.Result = resultBytes
		select {
		case m.messages <- response:
		default:
		}
	}

	if msg.Method == "prompts/get" {
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := GetPromptResult{
			Description: "prompt one",
			Messages: []PromptMessage{
				{Role: "user", Content: PromptContent{Type: "text", Text: "hello"}},
			},
		}
		resultBytes, _ := json.Marshal(result)
		response.Result = resultBytes
		select {
		case m.messages <- response:
		default:
		}
	}

	// Simulate initialize response
	if msg.Method == "initialize" {
		_ = json.Unmarshal(msg.Params, &m.initializeParams)
		response := &MCPMessage{
			JSONRpc: "2.0",
			ID:      msg.ID,
		}

		result := InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Instructions:    "Use the test tools carefully.",
			ServerInfo: ServerInfo{
				Name:    "test-server",
				Version: "1.0.0",
			},
			Capabilities: ServerCapabilities{
				Tools: &ToolsCapability{},
			},
		}

		resultBytes, _ := json.Marshal(result)
		response.Result = resultBytes

		select {
		case m.messages <- response:
		default:
		}
	}

	return nil
}

func (m *mockTransport) sentMessages() []*MCPMessage {
	m.sentMu.Lock()
	defer m.sentMu.Unlock()
	out := make([]*MCPMessage, len(m.sent))
	copy(out, m.sent)
	return out
}

func TestMCPClientDefaultCapabilitiesMatchTypeScriptEmptyObject(t *testing.T) {
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if transport.initializeParams.Capabilities.Experimental != nil ||
		transport.initializeParams.Capabilities.Extensions != nil ||
		transport.initializeParams.Capabilities.Roots != nil ||
		transport.initializeParams.Capabilities.Sampling != nil ||
		transport.initializeParams.Capabilities.Elicitation != nil {
		t.Fatalf("default capabilities = %#v, want empty object", transport.initializeParams.Capabilities)
	}
}

func TestMCPClientClientNameFallbacksAndNegotiatedProtocol(t *testing.T) {
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{Name: "DeprecatedMCPServer"})

	if client.clientInfo.Name != "DeprecatedMCPServer" {
		t.Fatalf("client name = %q", client.clientInfo.Name)
	}

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if transport.protocolVersion != ProtocolVersion {
		t.Fatalf("negotiated protocolVersion = %q, want %q", transport.protocolVersion, ProtocolVersion)
	}
}

func TestMCPClientRespondsToPingRequestWithEmptyResult(t *testing.T) {
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	transport.messages <- &MCPMessage{JSONRpc: "2.0", ID: "server-ping-1", Method: "ping"}

	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for ping response")
		default:
		}
		for _, msg := range transport.sentMessages() {
			if msg.ID == "server-ping-1" {
				if msg.Error != nil {
					t.Fatalf("ping response error = %#v", msg.Error)
				}
				var result map[string]interface{}
				if err := json.Unmarshal(msg.Result, &result); err != nil {
					t.Fatalf("ping result unmarshal error = %v, raw=%s", err, string(msg.Result))
				}
				if len(result) != 0 {
					t.Fatalf("ping result = %#v, want empty object", result)
				}
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMCPClientDoesNotRespondToPingNotification(t *testing.T) {
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	before := len(transport.sentMessages())
	transport.messages <- &MCPMessage{JSONRpc: "2.0", Method: "ping"}
	time.Sleep(20 * time.Millisecond)
	after := len(transport.sentMessages())
	if after != before {
		t.Fatalf("sent message count after ping notification = %d, want %d", after, before)
	}
}

func TestMCPToolConverterMetadataIncludesClientNameAndApp(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{ClientName: "MyMCPClient"})
	client.serverInfo = ServerInfo{Name: "test-server", Version: "1.0.0"}

	converter := NewMCPToolConverter(client)
	tool, err := converter.convertTool(MCPTool{
		Name:        "showDashboard",
		Title:       "Show Dashboard",
		Description: "Show dashboard",
		InputSchema: map[string]interface{}{"type": "object"},
		Meta: map[string]interface{}{"ui": map[string]interface{}{
			"resourceUri": "ui://ai-sdk-e2e/dashboard",
			"visibility":  []interface{}{"model", "app"},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("convertTool error: %v", err)
	}

	mcpMeta, ok := tool.ProviderMetadata["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing mcp metadata: %#v", tool.ProviderMetadata)
	}
	if mcpMeta["clientName"] != "MyMCPClient" || mcpMeta["toolName"] != "showDashboard" || mcpMeta["title"] != "Show Dashboard" {
		t.Fatalf("metadata = %#v", mcpMeta)
	}
	app, ok := mcpMeta["app"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing app metadata: %#v", mcpMeta)
	}
	if app["resourceUri"] != "ui://ai-sdk-e2e/dashboard" || app["mimeType"] != MCPAppMimeType {
		t.Fatalf("app metadata = %#v", app)
	}
}

func TestMCPClient_CallToolResourcesAndPrompts(t *testing.T) {
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{
		ClientName:    "test-client",
		ClientVersion: "1.0.0",
	})

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close() //nolint:errcheck

	toolRes, err := client.CallTool(ctx, "test-tool", map[string]interface{}{"input": "x"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(toolRes.Content) != 1 || toolRes.Content[0].Text != "tool-ok" {
		t.Fatalf("CallTool content = %#v", toolRes.Content)
	}

	resources, err := client.ListResources(ctx)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}
	if len(resources) != 1 || resources[0].Name != "r1" {
		t.Fatalf("ListResources = %#v", resources)
	}

	resource, err := client.ReadResource(ctx, "file://r1.txt")
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}
	if len(resource.Contents) != 1 || resource.Contents[0].Text != "hello" {
		t.Fatalf("ReadResource contents = %#v", resource.Contents)
	}

	prompts, err := client.ListPrompts(ctx)
	if err != nil {
		t.Fatalf("ListPrompts failed: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "p1" {
		t.Fatalf("ListPrompts = %#v", prompts)
	}

	prompt, err := client.GetPrompt(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("GetPrompt failed: %v", err)
	}
	if len(prompt.Messages) != 1 || prompt.Messages[0].Content.Text != "hello" {
		t.Fatalf("GetPrompt messages = %#v", prompt.Messages)
	}

	if client.ServerCapabilities().Tools == nil {
		t.Fatalf("ServerCapabilities() not populated: %#v", client.ServerCapabilities())
	}
}

func (m *mockTransport) Receive(ctx context.Context) (*MCPMessage, error) {
	select {
	case msg, ok := <-m.messages:
		if !ok {
			return nil, context.Canceled
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestGetSerializableTools(t *testing.T) {
	// Create client with mock transport
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{
		ClientName:    "test-client",
		ClientVersion: "1.0.0",
	})

	ctx := context.Background()

	// Connect and initialize
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close() //nolint:errcheck

	// Test GetSerializableTools
	result, err := client.GetSerializableTools(ctx)
	if err != nil {
		t.Fatalf("GetSerializableTools failed: %v", err)
	}

	// Verify result is not nil
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify tools are present
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}

	// Verify tool details
	tool := result.Tools[0]
	if tool.Name != "test-tool" {
		t.Errorf("expected tool name 'test-tool', got '%s'", tool.Name)
	}
	if tool.Description != "A test tool" {
		t.Errorf("expected description 'A test tool', got '%s'", tool.Description)
	}

	// Verify pagination cursor
	if result.NextCursor != "next-page-cursor" {
		t.Errorf("expected NextCursor 'next-page-cursor', got '%s'", result.NextCursor)
	}
	if client.ServerInstructions() != "Use the test tools carefully." {
		t.Errorf("expected server instructions to be preserved, got %q", client.ServerInstructions())
	}

	// Test serialization
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	// Test deserialization
	var deserialized ListToolsResult
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Verify deserialized data matches original
	if len(deserialized.Tools) != len(result.Tools) {
		t.Errorf("deserialized tools count mismatch: expected %d, got %d",
			len(result.Tools), len(deserialized.Tools))
	}
	if deserialized.NextCursor != result.NextCursor {
		t.Errorf("deserialized NextCursor mismatch: expected '%s', got '%s'",
			result.NextCursor, deserialized.NextCursor)
	}
}

func TestGetSerializableToolsNotInitialized(t *testing.T) {
	// Create client without connecting
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{
		ClientName:    "test-client",
		ClientVersion: "1.0.0",
	})

	ctx := context.Background()

	// Try to call GetSerializableTools without initializing
	_, err := client.GetSerializableTools(ctx)
	if err == nil {
		t.Fatal("expected error when calling GetSerializableTools without initialization")
	}

	if err.Error() != "client not initialized" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestGetSerializableToolsVsListTools(t *testing.T) {
	// Create client with mock transport
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{
		ClientName:    "test-client",
		ClientVersion: "1.0.0",
	})

	ctx := context.Background()

	// Connect and initialize
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close() //nolint:errcheck

	// Get serializable tools
	serializableResult, err := client.GetSerializableTools(ctx)
	if err != nil {
		t.Fatalf("GetSerializableTools failed: %v", err)
	}

	// Get tools using ListTools
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	// Verify both return the same tools
	if len(serializableResult.Tools) != len(tools) {
		t.Errorf("tool count mismatch: GetSerializableTools=%d, ListTools=%d",
			len(serializableResult.Tools), len(tools))
	}

	// Verify tool names match
	for i, tool := range tools {
		if tool.Name != serializableResult.Tools[i].Name {
			t.Errorf("tool name mismatch at index %d: expected '%s', got '%s'",
				i, serializableResult.Tools[i].Name, tool.Name)
		}
	}

	// GetSerializableTools should have pagination info, ListTools doesn't expose it
	if serializableResult.NextCursor == "" {
		t.Error("GetSerializableTools should include NextCursor for pagination")
	}
}

// pagedToolsTransport serves tools/list across two pages, keyed by the
// requested cursor, for testing ConvertToGoAITools' full pagination fetch
// (TS MCPClient.tools(), hash 1175434).
type pagedToolsTransport struct {
	messages  chan *MCPMessage
	connected bool
	mu        sync.Mutex
	cursors   []string
}

func newPagedToolsTransport() *pagedToolsTransport {
	return &pagedToolsTransport{messages: make(chan *MCPMessage, 10)}
}

func (t *pagedToolsTransport) Connect(ctx context.Context) error { t.connected = true; return nil }
func (t *pagedToolsTransport) Close() error {
	t.connected = false
	if t.messages != nil {
		close(t.messages)
	}
	return nil
}
func (t *pagedToolsTransport) IsConnected() bool           { return t.connected }
func (t *pagedToolsTransport) SetProtocolVersion(_ string) {}

func (t *pagedToolsTransport) Send(ctx context.Context, msg *MCPMessage) error {
	switch msg.Method {
	case "initialize":
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := InitializeResult{ProtocolVersion: ProtocolVersion, ServerInfo: ServerInfo{Name: "paged-server", Version: "1.0.0"}}
		data, _ := json.Marshal(result)
		response.Result = data
		t.messages <- response
	case "tools/list":
		var params ListToolsParams
		_ = json.Unmarshal(msg.Params, &params)
		t.mu.Lock()
		t.cursors = append(t.cursors, params.Cursor)
		t.mu.Unlock()

		var result ListToolsResult
		switch params.Cursor {
		case "":
			result = ListToolsResult{
				Tools:      []MCPTool{{Name: "tool-page-1", InputSchema: map[string]interface{}{"type": "object"}}},
				NextCursor: "page-2",
			}
		case "page-2":
			result = ListToolsResult{
				Tools: []MCPTool{{Name: "tool-page-2", InputSchema: map[string]interface{}{"type": "object"}}},
			}
		}
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		data, _ := json.Marshal(result)
		response.Result = data
		t.messages <- response
	}
	return nil
}

func (t *pagedToolsTransport) Receive(ctx context.Context) (*MCPMessage, error) {
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

func TestConvertToGoAIToolsFetchesAllPaginatedPages(t *testing.T) {
	transport := newPagedToolsTransport()
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	tools, err := GetMCPToolsForAgent(context.Background(), client)
	if err != nil {
		t.Fatalf("GetMCPToolsForAgent failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools = %#v, want 2 tools across both pages", tools)
	}
	names := map[string]bool{tools[0].Name: true, tools[1].Name: true}
	if !names["tool-page-1"] || !names["tool-page-2"] {
		t.Fatalf("tool names = %v, want tool-page-1 and tool-page-2", names)
	}

	transport.mu.Lock()
	cursors := append([]string(nil), transport.cursors...)
	transport.mu.Unlock()
	if len(cursors) != 2 || cursors[0] != "" || cursors[1] != "page-2" {
		t.Fatalf("cursors seen = %#v, want [\"\", \"page-2\"]", cursors)
	}
}

// retryableCallTransport fails the first failFirstN "tools/call" attempts at
// the transport level (simulating a connection error), then succeeds. Used to
// exercise MCPClientConfig.MaxRetries (TS mcp-client.ts callToolWithRetry,
// hash 8c616f0).
type retryableCallTransport struct {
	messages     chan *MCPMessage
	connected    bool
	mu           sync.Mutex
	callAttempts int
	failFirstN   int
	failErr      error
}

func newRetryableCallTransport(failFirstN int, failErr error) *retryableCallTransport {
	return &retryableCallTransport{messages: make(chan *MCPMessage, 10), failFirstN: failFirstN, failErr: failErr}
}

func (t *retryableCallTransport) Connect(ctx context.Context) error { t.connected = true; return nil }
func (t *retryableCallTransport) Close() error {
	t.connected = false
	if t.messages != nil {
		close(t.messages)
	}
	return nil
}
func (t *retryableCallTransport) IsConnected() bool           { return t.connected }
func (t *retryableCallTransport) SetProtocolVersion(_ string) {}

func (t *retryableCallTransport) Send(ctx context.Context, msg *MCPMessage) error {
	switch msg.Method {
	case "initialize":
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := InitializeResult{ProtocolVersion: ProtocolVersion, ServerInfo: ServerInfo{Name: "retry-server", Version: "1.0.0"}}
		data, _ := json.Marshal(result)
		response.Result = data
		t.messages <- response
	case "tools/call":
		t.mu.Lock()
		t.callAttempts++
		attempt := t.callAttempts
		t.mu.Unlock()
		if attempt <= t.failFirstN {
			return t.failErr
		}
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := CallToolResult{Content: []ToolResultContent{{Type: "text", Text: "ok"}}}
		data, _ := json.Marshal(result)
		response.Result = data
		t.messages <- response
	}
	return nil
}

func (t *retryableCallTransport) Receive(ctx context.Context) (*MCPMessage, error) {
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

func (t *retryableCallTransport) attempts() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.callAttempts
}

// TestCallToolRetriesTransientTransportFailures mirrors TS mcp-client.test.ts
// "should retry network-style tool call failures when maxRetries is
// configured".
func TestCallToolRetriesTransientTransportFailures(t *testing.T) {
	transport := newRetryableCallTransport(1, fmt.Errorf("dial tcp 127.0.0.1:1234: connect: %w", syscall.ECONNREFUSED))
	client := NewMCPClient(transport, MCPClientConfig{MaxRetries: 1})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	result, err := client.CallTool(context.Background(), "retry-tool", nil)
	if err != nil {
		t.Fatalf("CallTool failed after retry: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("result = %#v", result)
	}
	if transport.attempts() != 2 {
		t.Fatalf("attempts = %d, want 2 (1 failure + 1 retry)", transport.attempts())
	}
}

// TestCallToolDoesNotRetryByDefault mirrors TS mcp-client.test.ts "should not
// retry tool calls by default".
func TestCallToolDoesNotRetryByDefault(t *testing.T) {
	transport := newRetryableCallTransport(1, fmt.Errorf("dial tcp 127.0.0.1:1234: connect: %w", syscall.ECONNREFUSED))
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if _, err := client.CallTool(context.Background(), "retry-tool", nil); err == nil {
		t.Fatal("expected CallTool to fail without a retry")
	}
	if transport.attempts() != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry by default)", transport.attempts())
	}
}

// TestConnectRejectsNegativeMaxRetries mirrors TS mcp-client.ts's
// prepareMaxRetries, which throws MCPClientError("maxRetries must be >= 0")
// synchronously from the constructor for a negative maxRetries. Go's
// constructor (NewMCPClient) cannot fail, so the check happens at the first
// fallible call, Connect.
func TestConnectRejectsNegativeMaxRetries(t *testing.T) {
	transport := newRetryableCallTransport(0, nil)
	client := NewMCPClient(transport, MCPClientConfig{MaxRetries: -1})
	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected Connect to reject a negative MaxRetries")
	}
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) {
		t.Fatalf("error = %T, want *MCPClientError", err)
	}
	if transport.connected {
		t.Fatal("transport should not have been connected after validation failure")
	}
}

// TestIsRetryableMCPToolCallError mirrors the retry classification in TS
// mcp-client.ts isRetryableMCPToolCallError and mcp-client.test.ts's "should
// not retry HTTP status codes outside the default retry list" / "should not
// retry invalid argument JSON-RPC errors" / "should not retry auth failures".
func TestIsRetryableMCPToolCallError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"HTTP 429 is retryable", &MCPClientError{StatusCode: 429}, true},
		{"HTTP 500 is retryable", &MCPClientError{StatusCode: 500}, true},
		{"HTTP 408 is retryable", &MCPClientError{StatusCode: 408}, true},
		{"HTTP 409 is retryable", &MCPClientError{StatusCode: 409}, true},
		{"HTTP 400 is not retryable", &MCPClientError{StatusCode: 400}, false},
		{"HTTP 404 is not retryable", &MCPClientError{StatusCode: 404}, false},
		{"HTTP 401 auth failure is not retryable", &MCPClientError{StatusCode: 401}, false},
		{"JSON-RPC invalid params error is not retryable", &MCPClientError{Code: ErrorCodeInvalidParams, Message: "bad args"}, false},
		{"JSON-RPC tool-not-found error is not retryable", &MCPClientError{Code: ErrorCodeToolNotFound}, false},
		{"connection refused is retryable", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), true},
		{"connection reset is retryable", fmt.Errorf("read: %w", syscall.ECONNRESET), true},
		{"generic error is not retryable", fmt.Errorf("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableMCPToolCallError(tc.err); got != tc.want {
				t.Fatalf("isRetryableMCPToolCallError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
