package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// MCPToolConverter converts MCP tools to Go-AI tools
type MCPToolConverter struct {
	client *MCPClient
}

// NewMCPToolConverter creates a new MCP tool converter
func NewMCPToolConverter(client *MCPClient) *MCPToolConverter {
	return &MCPToolConverter{
		client: client,
	}
}

// ConvertToGoAITools fetches MCP tools and converts them to Go-AI tools
func (c *MCPToolConverter) ConvertToGoAITools(ctx context.Context) ([]types.Tool, error) {
	// List tools from MCP server
	mcpTools, err := c.client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP tools: %w", err)
	}

	// Convert each MCP tool to Go-AI tool
	goaiTools := make([]types.Tool, len(mcpTools))
	for i, mcpTool := range mcpTools {
		goaiTools[i] = c.convertTool(mcpTool)
	}

	return goaiTools, nil
}

// convertTool converts a single MCP tool to a Go-AI tool
func (c *MCPToolConverter) convertTool(mcpTool MCPTool) types.Tool {
	return types.Tool{
		Name:        mcpTool.Name,
		Description: mcpTool.Description,
		Parameters:  mcpTool.InputSchema,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			// Call MCP tool
			result, err := c.client.CallTool(ctx, mcpTool.Name, input)
			if err != nil {
				return nil, fmt.Errorf("MCP tool execution failed: %w", err)
			}

			// Check if the tool returned an error
			if result.IsError {
				return nil, fmt.Errorf("tool returned error: %v", result.Content)
			}

			// Convert MCP content to AI SDK content parts
			// This properly handles images to prevent 200K+ token explosions
			contentParts, err := ConvertMCPContentToAISDK(result.Content)
			if err != nil {
				return nil, fmt.Errorf("failed to convert MCP content: %w", err)
			}

			return contentParts, nil
		},
		// Mark this as provider-executed since it's executed via MCP
		ToModelOutput: func(ctx context.Context, options types.ToModelOutputOptions) (types.ToolResultOutput, error) {
			// Convert MCP result to model-readable format
			return c.convertToModelOutput(options.Result), nil
		},
	}
}

// convertToModelOutput converts a tool result to model-readable output
func (c *MCPToolConverter) convertToModelOutput(result interface{}) types.ToolResultOutput {
	// Result is already []types.ContentPart from Execute function
	// The AI SDK will handle the content parts directly
	contentParts, ok := result.([]types.ContentPart)
	if !ok {
		// Fallback for backward compatibility
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			return types.ToolResultOutput{
				Type:    "text",
				Content: fmt.Sprintf("%v", result),
			}
		}
		return types.ToolResultOutput{
			Type:    "text",
			Content: string(jsonBytes),
		}
	}

	// Return content parts as structured data
	// Images will be handled properly, preventing the 200K+ token explosion bug
	return types.ToolResultOutput{
		Type: "custom",
		Data: contentParts,
	}
}

// GetMCPToolsForAgent fetches and converts MCP tools for use with agents
func GetMCPToolsForAgent(ctx context.Context, client *MCPClient) ([]types.Tool, error) {
	converter := NewMCPToolConverter(client)
	return converter.ConvertToGoAITools(ctx)
}

// CreateMCPClient creates an MCP client with the specified configuration
// This is a convenience function for quickly setting up MCP connections
func CreateMCPClient(config MCPClientConfig, transport Transport) (*MCPClient, error) {
	client := NewMCPClient(transport, config)
	return client, nil
}

// CreateStdioMCPClient creates an MCP client with stdio transport
// This is useful for connecting to local MCP servers
//
// Example:
//
//	client, err := CreateStdioMCPClient("python", []string{"-m", "mcp_server"})
//	if err != nil {
//	    // handle error
//	}
//	defer client.Close()
//
//	if err := client.Connect(ctx); err != nil {
//	    // handle error
//	}
//
//	tools, err := GetMCPToolsForAgent(ctx, client)
//	if err != nil {
//	    // handle error
//	}
func CreateStdioMCPClient(command string, args []string) (*MCPClient, error) {
	transport := NewStdioTransport(StdioTransportConfig{
		Command: command,
		Args:    args,
		Config: TransportConfig{
			EnableLogging: false,
		},
	})

	config := MCPClientConfig{
		ClientName:       "go-ai-mcp-client",
		ClientVersion:    "1.0.0",
		RequestTimeoutMS: 30000,
		EnableLogging:    false,
	}

	return CreateMCPClient(config, transport)
}

// CreateHTTPMCPClient creates an MCP client with HTTP transport
// This is useful for connecting to remote MCP servers
//
// Example:
//
//	client, err := CreateHTTPMCPClient("https://mcp.example.com", nil)
//	if err != nil {
//	    // handle error
//	}
//	defer client.Close()
//
//	if err := client.Connect(ctx); err != nil {
//	    // handle error
//	}
//
//	tools, err := GetMCPToolsForAgent(ctx, client)
//	if err != nil {
//	    // handle error
//	}
func CreateHTTPMCPClient(url string, oauth *OAuthConfig) (*MCPClient, error) {
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       url,
		TimeoutMS: 30000,
		OAuth:     oauth,
		Config: TransportConfig{
			EnableLogging: false,
		},
	})

	config := MCPClientConfig{
		ClientName:       "go-ai-mcp-client",
		ClientVersion:    "1.0.0",
		RequestTimeoutMS: 30000,
		EnableLogging:    false,
	}

	return CreateMCPClient(config, transport)
}
