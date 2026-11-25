package googlevertex

import "testing"

// TS b0fccbda62: googleVertex.tools.mcpServer re-exports google's.
func TestPortprovgVertexMCPServerTool(t *testing.T) {
	tool := MCPServerTool(map[string]interface{}{"url": "https://example.com/mcp"})
	if tool.ProviderID != "google.mcp_server" || tool.Name != "mcp_server" {
		t.Fatalf("tool = %+v", tool)
	}
}
