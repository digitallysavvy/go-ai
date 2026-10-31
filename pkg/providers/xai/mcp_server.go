package xai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// MCPServerConfig contains configuration for the xAI MCP Server tool
type MCPServerConfig struct {
	// ServerURL is the URL of the remote MCP server to connect to
	ServerURL string

	// ServerLabel is a short display label for this MCP server.
	ServerLabel string

	// ServerDescription describes what this MCP server provides.
	ServerDescription string

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

// MCPServerOptions holds the typed config used by prepareXAIResponsesTools to serialize
// the mcp tool into the Responses API request body.
type MCPServerOptions struct {
	ServerURL         string
	ServerLabel       string
	ServerDescription string
	AllowedTools      []string
	Headers           map[string]string
	Authorization     string
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
	// TS xai mcpServer: inputSchema z.object({}). The server URL, allowed
	// tools, headers and authorization are tool args (ProviderOptions), not
	// model input, so they are not in the input schema (provider tool calls
	// are validated against it, and its defaults would be merged into them).
	parameters := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}

	args := map[string]interface{}{"serverUrl": config.ServerURL}
	if config.ServerLabel != "" {
		args["serverLabel"] = config.ServerLabel
	}
	if config.ServerDescription != "" {
		args["serverDescription"] = config.ServerDescription
	}
	if config.AllowedTools != nil {
		args["allowedTools"] = config.AllowedTools
	}
	if config.Headers != nil {
		args["headers"] = config.Headers
	}
	if config.Authorization != "" {
		// TS sends authorization in the args (also to the AI Gateway).
		args["authorization"] = config.Authorization
	}
	return xaiProviderTool(types.Tool{
		Name:        "xai.mcp",
		Description: "Connect to remote MCP (Model Context Protocol) server and execute tools. Enables integration with external services and APIs through MCP-compatible servers.",
		Title:       "MCP Server",
		Parameters:  parameters,
		// TS outputSchema z.object({name: z.string(), arguments: z.string(),
		// result: z.unknown()}) (ValidateUIMessages checks outputs with it).
		OutputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":      map[string]interface{}{"type": "string"},
				"arguments": map[string]interface{}{"type": "string"},
				"result":    map[string]interface{}{},
			},
			"required": []interface{}{"name", "arguments", "result"},
		},
		// ProviderOptions carries the typed config for API serialization by prepareXAIResponsesTools.
		ProviderOptions: MCPServerOptions(config),
		// This tool is executed by xAI's servers, not locally
		ProviderExecuted: true,
		Execute:          providerExecutedNoop("xai.mcp"),
	}, args)
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
