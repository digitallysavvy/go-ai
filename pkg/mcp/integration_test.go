package mcp

import (
	"testing"
)

func TestMCPToolConverterPropagatesServerNameMetadata(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	client.serverInfo = ServerInfo{Name: "filesystem", Version: "1.0.0"}

	converter := NewMCPToolConverter(client)
	tool := converter.convertTool(MCPTool{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	})

	if tool.ProviderName != "mcp" {
		t.Fatalf("ProviderName = %q, want mcp", tool.ProviderName)
	}
	mcpMeta, ok := tool.ProviderMetadata["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing mcp provider metadata: %#v", tool.ProviderMetadata)
	}
	if mcpMeta["serverName"] != "filesystem" {
		t.Fatalf("serverName = %v, want filesystem", mcpMeta["serverName"])
	}
}
