package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// mockTransport implements Transport interface for testing
type mockTransport struct {
	messages  chan *MCPMessage
	connected bool
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		messages: make(chan *MCPMessage, 10),
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

func (m *mockTransport) Send(ctx context.Context, msg *MCPMessage) error {
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

	// Simulate initialize response
	if msg.Method == "initialize" {
		response := &MCPMessage{
			JSONRpc: "2.0",
			ID:      msg.ID,
		}

		result := InitializeResult{
			ProtocolVersion: ProtocolVersion,
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
	defer client.Close()

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
	defer client.Close()

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
