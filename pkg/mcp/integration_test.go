package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMCPToolConverterPropagatesMcpProviderMetadata(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	client.serverInfo = ServerInfo{Name: "filesystem", Version: "1.0.0"}

	converter := NewMCPToolConverter(client)
	tool, err := converter.convertTool(MCPTool{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	})
	if err != nil {
		t.Fatalf("convertTool error: %v", err)
	}

	if tool.ProviderName != "mcp" {
		t.Fatalf("ProviderName = %q, want mcp", tool.ProviderName)
	}
	mcpMeta, ok := tool.ProviderMetadata["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing mcp provider metadata: %#v", tool.ProviderMetadata)
	}
	if mcpMeta["clientName"] != "ai-sdk-mcp-client" || mcpMeta["toolName"] != "read_file" {
		t.Fatalf("mcp metadata = %#v", mcpMeta)
	}
	if _, ok := mcpMeta["serverName"]; ok {
		t.Fatalf("serverName should not be present in TS-parity McpProviderMetadata: %#v", mcpMeta)
	}
}

func TestMCPToolConverterModelOutputFallbackAndFactoryHelpers(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	out := converter.convertToModelOutput(map[string]interface{}{"a": 1})
	if out.Type != types.ToolResultOutputText {
		t.Fatalf("fallback output type = %q, want text", out.Type)
	}

	contentOut := converter.convertToModelOutput([]types.ContentPart{
		types.TextContent{Text: "hello"},
		types.ImageContent{Image: []byte{1, 2}, MimeType: "image/png"},
		types.FileContent{Data: []byte{3}, MimeType: "application/octet-stream", Filename: "x.bin"},
	})
	if contentOut.Type != types.ToolResultOutputContent || len(contentOut.Content) != 3 {
		t.Fatalf("content output = %#v", contentOut)
	}

	mcpClient, err := CreateMCPClient(MCPClientConfig{}, newMockTransport())
	if err != nil || mcpClient == nil {
		t.Fatalf("CreateMCPClient() = (%v, %v)", mcpClient, err)
	}

	stdioClient, err := CreateStdioMCPClient("cat", nil)
	if err != nil || stdioClient == nil {
		t.Fatalf("CreateStdioMCPClient() = (%v, %v)", stdioClient, err)
	}
	httpClient, err := CreateHTTPMCPClient("https://example.com/mcp", nil)
	if err != nil || httpClient == nil {
		t.Fatalf("CreateHTTPMCPClient() = (%v, %v)", httpClient, err)
	}
}

func TestMCPToolConverterInvalidAppMetadataReturnsError(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	_, err := converter.convertTool(MCPTool{
		Name:        "bad_app",
		InputSchema: map[string]interface{}{"type": "object"},
		Meta: map[string]interface{}{"ui": map[string]interface{}{
			"resourceUri": "https://example.com/app.html",
		}},
	})
	if err == nil {
		t.Fatal("expected invalid MCP App resource URI error")
	}
}

type listToolErrorClient struct{}

func (listToolErrorClient) ListTools(context.Context) ([]MCPTool, error) {
	return nil, errors.New("boom")
}

func TestConvertToGoAIToolsErrorPath(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	client.initialized = true
	converter := NewMCPToolConverter(client)

	// Force conversion execution path by setting a non-empty tool and calling Execute.
	client.serverInfo = ServerInfo{Name: "server"}
	tool, err := converter.convertTool(MCPTool{
		Name:        "echo",
		Description: "echo",
		InputSchema: map[string]interface{}{"type": "object"},
	})
	if err != nil {
		t.Fatalf("convertTool error: %v", err)
	}

	_, err = tool.Execute(context.Background(), map[string]interface{}{}, types.ToolExecutionOptions{})
	if err == nil {
		t.Fatal("expected Execute to fail when mock transport isn't connected")
	}

	if _, err := GetMCPToolsForAgent(context.Background(), client); err == nil {
		// A disconnected mock client should fail ListTools and cover error wrapping path.
		t.Fatal("GetMCPToolsForAgent() expected error on disconnected client")
	}
}
