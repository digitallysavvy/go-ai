package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// TS applyInitializeResult throws an MCPClientError with this exact text.
func TestMCPClientUnsupportedProtocolVersionIsMCPClientError(t *testing.T) {
	bad := InitializeResult{ProtocolVersion: "1999-01-01"}
	err := NewMCPClient(newLifecycleTransport(ServerCapabilities{}), MCPClientConfig{InitialInitializeResult: &bad}).Connect(context.Background())
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "Server's protocol version is not supported: 1999-01-01" {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

// A request pending when the client closes fails with TS onClose's
// "Connection closed" MCPClientError.
func TestMCPClientCloseRejectsPendingWithConnectionClosed(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{Tools: &ToolsCapability{}})
	client := NewMCPClient(tr, MCPClientConfig{InitialInitializeResult: &InitializeResult{ProtocolVersion: ProtocolVersion, Capabilities: ServerCapabilities{Tools: &ToolsCapability{}}}})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := client.ListTools(context.Background())
		errc <- err
	}()
	for len(tr.methods()) == 0 {
	}
	_ = client.Close()
	err := <-errc
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || (mcpErr.Message != "Connection closed" && mcpErr.Message != "Attempted to send a request from a closed client") {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

// TS extractStructuredContent uses the first text part; validation errors
// carry the cause and quote the tool name literally.
func TestExtractMCPStructuredOutputFirstTextPart(t *testing.T) {
	out := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	res := CallToolResult{Content: []ToolResultContent{{Type: "text"}, {Type: "text", Text: `{"a":1}`}}}
	_, err := extractMCPStructuredOutput(res, out, `x"y`)
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != `Tool "x"y" did not return structuredContent or parseable text content` {
		t.Fatalf("err = %v", err)
	}
	res = CallToolResult{Content: []ToolResultContent{{Type: "text", Text: `[1]`}}}
	_, err = extractMCPStructuredOutput(res, out, "t")
	if !errors.As(err, &mcpErr) || mcpErr.Cause == nil || mcpErr.Data != nil {
		t.Fatalf("err = %#v", err)
	}
}
