package tool

import "testing"

func TestMCP(t *testing.T) {
	readOnly := true
	tool := MCP(MCPConfig{
		ServerLabel:       "docs",
		ServerDescription: "Documentation tools",
		ServerURL:         "https://mcp.example.com",
		AllowedTools:      MCPAllowedTools{ReadOnly: &readOnly, ToolNames: []string{"search"}},
		RequireApproval:   MCPRequireApproval{Never: &MCPApprovalFilter{ToolNames: []string{"search"}}},
	})

	if tool.Name != "openai.mcp" {
		t.Fatalf("Name = %q, want openai.mcp", tool.Name)
	}
	if !tool.ProviderExecuted {
		t.Fatal("ProviderExecuted = false, want true")
	}
	if tool.Description != "Documentation tools" {
		t.Fatalf("Description = %q", tool.Description)
	}
	cfg, ok := tool.ProviderOptions.(MCPConfig)
	if !ok {
		t.Fatalf("ProviderOptions = %T, want MCPConfig", tool.ProviderOptions)
	}
	if cfg.ServerLabel != "docs" || cfg.ServerURL != "https://mcp.example.com" {
		t.Fatalf("config = %#v", cfg)
	}
}
