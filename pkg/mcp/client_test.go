package mcp

import (
	"context"
	"encoding/json"
	"sync"
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
