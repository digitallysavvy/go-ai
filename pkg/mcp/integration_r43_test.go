package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type toolErrorTransport struct{ *mockTransport }

func (m toolErrorTransport) Send(ctx context.Context, msg *MCPMessage) error {
	if msg.Method != "tools/call" {
		return m.mockTransport.Send(ctx, msg)
	}
	m.messages <- &MCPMessage{JSONRpc: "2.0", ID: msg.ID, Error: &MCPError{Code: -32602, Message: "Unknown tool: echo"}}
	return nil
}

// TS execute propagates the callTool error unchanged: the tool error the
// model sees is the server's message, with no wrapper prefix.
func TestMCPToolExecuteReturnsCallToolErrorUnwrapped(t *testing.T) {
	client := NewMCPClient(toolErrorTransport{newMockTransport()}, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = client.Close() }()
	tool, err := NewMCPToolConverter(client).convertTool(MCPTool{Name: "echo", InputSchema: map[string]interface{}{"type": "object"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Execute(context.Background(), map[string]interface{}{}, types.ToolExecutionOptions{})
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Code != -32602 {
		t.Fatalf("got %T %v", err, err)
	}
	if err.Error() != "Unknown tool: echo" {
		t.Fatalf("error text = %q", err.Error())
	}
}

// TS lets request errors propagate unchanged from every client method and
// from init(); a client that is not connected rejects with TS request()'s
// MCPClientError.
func TestMCPClientErrorsCarryTSTextWithoutGoPrefixes(t *testing.T) {
	bad := InitializeResult{ProtocolVersion: "1999-01-01"}
	err := NewMCPClient(newLifecycleTransport(ServerCapabilities{}), MCPClientConfig{InitialInitializeResult: &bad}).Connect(context.Background())
	if err == nil || err.Error() != "Server's protocol version is not supported: 1999-01-01" {
		t.Fatalf("Connect err = %v", err)
	}

	notConnected := NewMCPClient(newMockTransport(), MCPClientConfig{})
	calls := map[string]func() error{
		"ListTools":     func() error { _, err := notConnected.ListTools(context.Background()); return err },
		"CallTool":      func() error { _, err := notConnected.CallTool(context.Background(), "x", nil); return err },
		"ListResources": func() error { _, err := notConnected.ListResources(context.Background()); return err },
		"GetPrompt":     func() error { _, err := notConnected.GetPrompt(context.Background(), "p", nil); return err },
	}
	for name, call := range calls {
		err := call()
		var mcpErr *MCPClientError
		if !errors.As(err, &mcpErr) || err.Error() != "Attempted to send a request from a closed client" {
			t.Fatalf("%s err = %v (%T)", name, err, err)
		}
	}

	client := NewMCPClient(toolErrorTransport{newMockTransport()}, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if _, err := client.CallTool(context.Background(), "echo", nil); err == nil || err.Error() != "Unknown tool: echo" {
		t.Fatalf("CallTool err = %v", err)
	}
}
