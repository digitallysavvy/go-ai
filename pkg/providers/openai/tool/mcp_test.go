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
	if tool.ProviderID != "openai.mcp" {
		t.Fatalf("ProviderID = %q, want openai.mcp", tool.ProviderID)
	}
}

// TestMCP_CustomName covers item 4b: MCPConfig.Name lets a caller register
// more than one MCP server in the same request (Go tools are a flat slice
// keyed by Name, unlike TS's tools-object key), while ProviderID keeps
// wire/tool_choice routing working off the fixed "openai.mcp" identity.
func TestMCP_CustomName(t *testing.T) {
	docs := MCP(MCPConfig{ServerLabel: "docs", ServerURL: "https://docs.example.com", Name: "docsServer"})
	support := MCP(MCPConfig{ServerLabel: "support", ServerURL: "https://support.example.com", Name: "supportServer"})

	if docs.Name != "docsServer" || support.Name != "supportServer" {
		t.Fatalf("Names = %q, %q, want distinct custom names", docs.Name, support.Name)
	}
	if docs.ProviderID != "openai.mcp" || support.ProviderID != "openai.mcp" {
		t.Fatalf("ProviderID = %q, %q, want openai.mcp for both", docs.ProviderID, support.ProviderID)
	}

	defaultNamed := MCP(MCPConfig{ServerLabel: "docs", ServerURL: "https://docs.example.com"})
	if defaultNamed.Name != "openai.mcp" {
		t.Fatalf("Name = %q, want default openai.mcp when unset", defaultNamed.Name)
	}
}
