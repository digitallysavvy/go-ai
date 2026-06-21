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
}

// MCP creates an OpenAI provider-executed MCP tool.
func MCP(config MCPConfig) types.Tool {
	return types.Tool{
		Name:             "openai.mcp",
		Description:      config.ServerDescription,
		ProviderExecuted: true,
		ProviderOptions:  config,
		Parameters:       map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}
