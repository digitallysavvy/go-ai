package xai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// MCPServerConfig contains configuration for the xAI MCP Server tool
type MCPServerConfig struct {
	// ServerURL is the URL of the remote MCP server to connect to
	ServerURL string

	// AllowedTools is a list of tool names that are allowed to be used from the MCP server
	// If empty, all tools from the MCP server are allowed
	AllowedTools []string

	// Headers contains custom HTTP headers to send with requests to the MCP server
	// Useful for authentication, API versioning, etc.
	Headers map[string]string

	// Authorization contains the authorization token or credentials
	// This will be set as the Authorization header
	Authorization string
}

// MCPServer creates a provider-executed tool for connecting to remote MCP servers
// This tool enables integration with external services through the Model Context Protocol (MCP)
//
// The MCP Server tool allows:
// - Remote tool execution through MCP-compatible servers
// - Filtering of available tools using AllowedTools
// - Custom headers and authorization for secure connections
// - Integration with external services and APIs
//
// Example:
//
//	tool := xai.MCPServer(xai.MCPServerConfig{
//	    ServerURL:     "https://api.example.com/mcp",
//	    AllowedTools:  []string{"search", "summarize"},
//	    Authorization: "Bearer " + apiKey,
//	    Headers: map[string]string{
//	        "X-API-Version": "v1",
//	    },
//	})
func MCPServer(config MCPServerConfig) types.Tool {
	// Build the parameters schema
	properties := map[string]interface{}{
		"serverUrl": map[string]interface{}{
			"type":        "string",
			"description": "URL of the MCP server to connect to",
			"default":     config.ServerURL,
		},
		"toolName": map[string]interface{}{
			"type":        "string",
			"description": "Name of the tool to execute on the MCP server",
		},
		"arguments": map[string]interface{}{
			"type":        "object",
			"description": "Arguments to pass to the MCP server tool",
		},
	}

	// Add optional parameters if configured
	if len(config.AllowedTools) > 0 {
		properties["allowedTools"] = map[string]interface{}{
			"type":        "array",
			"description": "List of tool names that are allowed to be used",
			"items": map[string]interface{}{
				"type": "string",
			},
			"default": config.AllowedTools,
		}
	}

	if len(config.Headers) > 0 {
		properties["headers"] = map[string]interface{}{
			"type":        "object",
			"description": "Custom HTTP headers to send with the request",
			"default":     config.Headers,
		}
	}

	if config.Authorization != "" {
		properties["authorization"] = map[string]interface{}{
			"type":        "string",
			"description": "Authorization token for the MCP server",
			"default":     config.Authorization,
		}
	}

	parameters := map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   []string{"serverUrl", "toolName"},
	}

	return types.Tool{
		Name:        "xai.mcp",
		Description: "Connect to remote MCP (Model Context Protocol) server and execute tools. Enables integration with external services and APIs through MCP-compatible servers.",
		Title:       "MCP Server",
		Parameters:  parameters,
		// This tool is executed by xAI's servers, not locally
		ProviderExecuted: true,
		// Execute function is not needed for provider-executed tools
		// The provider will handle execution and return results
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			// This should never be called for provider-executed tools
			// The provider handles execution
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "xai.mcp",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}
}

// MCPServerSimple creates an MCP Server tool with minimal configuration
// Only requires the server URL, allows all tools
func MCPServerSimple(serverURL string) types.Tool {
	return MCPServer(MCPServerConfig{
		ServerURL: serverURL,
	})
}

// MCPServerWithAuth creates an MCP Server tool with server URL and authorization
// Useful for authenticated MCP server connections
func MCPServerWithAuth(serverURL, authorization string) types.Tool {
	return MCPServer(MCPServerConfig{
		ServerURL:     serverURL,
		Authorization: authorization,
	})
}
