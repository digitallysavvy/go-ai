package tool

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// MCPAllowedTools filters the tools available from an OpenAI MCP server.
type MCPAllowedTools struct {
	ReadOnly  *bool    `json:"readOnly,omitempty"`
	ToolNames []string `json:"toolNames,omitempty"`
}

// MCPApprovalFilter identifies MCP tools that do not require approval.
type MCPApprovalFilter struct {
	ToolNames []string `json:"toolNames,omitempty"`
}

// MCPRequireApproval configures MCP approval filtering.
type MCPRequireApproval struct {
	Never *MCPApprovalFilter `json:"never,omitempty"`
}

// MCPConfig configures the OpenAI Responses MCP provider tool.
type MCPConfig struct {
	ServerLabel       string            `json:"serverLabel"`
	AllowedTools      interface{}       `json:"allowedTools,omitempty"`
	Authorization     string            `json:"authorization,omitempty"`
	ConnectorID       string            `json:"connectorId,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	RequireApproval   interface{}       `json:"requireApproval,omitempty"`
	ServerDescription string            `json:"serverDescription,omitempty"`
	ServerURL         string            `json:"serverUrl,omitempty"`

	// Name optionally overrides the tool's SDK name (default: "openai.mcp").
	// TS callers pick this name freely as the tools-object key (e.g.
	// `tools: { docsServer: mcp({...}), supportServer: mcp({...}) }`); Go
	// tools are a flat slice keyed by Name, so registering more than one MCP
	// server in the same request requires giving each one a distinct Name
	// here. The wire identity (routing, tool_choice, allowedTools) is keyed
	// off ProviderID ("openai.mcp"), which is always set regardless of Name.
	Name string `json:"-"`
}

// MCP creates an OpenAI provider-executed MCP tool. Set Name in config to
// register more than one MCP server in the same request (see MCPConfig.Name).
func MCP(config MCPConfig) types.Tool {
	name := config.Name
	if name == "" {
		name = "openai.mcp"
	}
	return types.Tool{
		Name:             name,
		ProviderID:       "openai.mcp",
		Description:      config.ServerDescription,
		ProviderExecuted: true,
		ProviderOptions:  config,
		Parameters:       map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}
