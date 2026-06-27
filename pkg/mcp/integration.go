package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// MCPToolConverter converts MCP tools to Go-AI tools
type MCPToolConverter struct {
	client *MCPClient
}

// MCPToolSchema supplies the Go equivalent of the TypeScript MCP schemas map.
// When provided, only matching MCP tools are converted. InputSchema replaces
// the server-discovered input schema, and OutputSchema validates structured
// tool execution output before it is returned to the caller.
type MCPToolSchema struct {
	InputSchema  interface{}
	OutputSchema interface{}
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

	return c.ToolsFromDefinitions(mcpTools, nil)
}

// ConvertToGoAIToolsWithSchemas fetches MCP tools and applies a schemas map,
// matching TypeScript client.tools({ schemas }). Tools not present in schemas
// are omitted, and per-tool input/output schemas override discovered schemas.
func (c *MCPToolConverter) ConvertToGoAIToolsWithSchemas(ctx context.Context, schemas map[string]MCPToolSchema) ([]types.Tool, error) {
	mcpTools, err := c.client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP tools: %w", err)
	}
	return c.ToolsFromDefinitions(mcpTools, schemas)
}

// ToolsFromDefinitions converts already-fetched MCP tool definitions, matching
// TypeScript toolsFromDefinitions. Passing nil schemas uses automatic schema
// discovery; a non-nil schemas map filters to own matching tool names.
func (c *MCPToolConverter) ToolsFromDefinitions(mcpTools []MCPTool, schemas map[string]MCPToolSchema) ([]types.Tool, error) {
	goaiTools := make([]types.Tool, 0, len(mcpTools))
	for _, mcpTool := range mcpTools {
		var toolSchema *MCPToolSchema
		if schemas != nil {
			s, ok := schemas[mcpTool.Name]
			if !ok {
				continue
			}
			toolSchema = &s
		}
		tool, err := c.convertTool(mcpTool, toolSchema)
		if err != nil {
			return nil, err
		}
		goaiTools = append(goaiTools, tool)
	}
	return goaiTools, nil
}

// convertTool converts a single MCP tool to a Go-AI tool
func (c *MCPToolConverter) convertTool(mcpTool MCPTool, toolSchema *MCPToolSchema) (types.Tool, error) {
	resolvedTitle, hasResolvedTitle := resolveMCPToolTitle(mcpTool)
	mcpMetadata := map[string]interface{}{
		"clientName": c.client.clientInfo.Name,
		"toolName":   mcpTool.Name,
	}
	if hasResolvedTitle {
		mcpMetadata["title"] = resolvedTitle
	}
	appMeta, err := GetMCPAppToolMeta(mcpTool)
	if err != nil {
		return types.Tool{}, err
	}
	if appMeta != nil && appMeta.ResourceURI != "" {
		app := copyMap(appMeta.Extra)
		app["mimeType"] = MCPAppMimeType
		mcpMetadata["app"] = app
	}
	providerMetadata := map[string]interface{}{"mcp": mcpMetadata}
	parameters := interface{}(normalizeAutomaticMCPInputSchema(mcpTool.InputSchema))
	var outputSchema schema.Schema
	toolType := types.ToolTypeDynamic
	if toolSchema != nil {
		toolType = types.ToolTypeFunction
		if toolSchema.InputSchema != nil {
			normalizedInputSchema, err := normalizeMCPInputSchema(toolSchema.InputSchema)
			if err != nil {
				return types.Tool{}, err
			}
			parameters = normalizedInputSchema
		}
		var err error
		outputSchema, err = normalizeMCPOutputSchema(toolSchema.OutputSchema)
		if err != nil {
			return types.Tool{}, err
		}
	}

	return types.Tool{
		Name:             mcpTool.Name,
		Description:      mcpTool.Description,
		Title:            resolvedTitle,
		Parameters:       parameters,
		OutputSchema:     outputSchema,
		ProviderName:     "mcp",
		ProviderMetadata: providerMetadata,
		Metadata:         mcpMetadata,
		Meta:             copyMap(mcpTool.Meta),
		Type:             toolType,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			// Call MCP tool
			result, err := c.client.CallTool(ctx, mcpTool.Name, input)
			if err != nil {
				return nil, fmt.Errorf("LMCP tool execution failed: %w", err)
			}
			if outputSchema != nil && !result.IsError {
				return extractMCPStructuredOutput(*result, outputSchema, mcpTool.Name)
			}
			return result, nil
		},
		// Mark this as provider-executed since it's executed via MCP
		ToModelOutput: func(ctx context.Context, options types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
			_ = ctx
			// Convert MCP result to model-readable format
			output := c.convertToModelOutput(options.Output)
			return &output, nil
		},
	}, nil
}

func stringFromMap(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func resolveMCPToolTitle(tool MCPTool) (string, bool) {
	if tool.titlePresent || tool.Title != "" {
		return tool.Title, true
	}
	if annotationsTitle, ok := tool.Annotations["title"].(string); ok {
		return annotationsTitle, true
	}
	return "", false
}

func normalizeAutomaticMCPInputSchema(input map[string]interface{}) map[string]interface{} {
	normalized := copyMap(input)
	if normalized == nil {
		normalized = map[string]interface{}{}
	}
	if properties, ok := normalized["properties"]; !ok || properties == nil {
		normalized["properties"] = map[string]interface{}{}
	}
	normalized["additionalProperties"] = false
	return normalized
}

func normalizeMCPInputSchema(value interface{}) (map[string]interface{}, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case schema.Schema:
		return copyMap(v.Validator().JSONSchema()), nil
	case map[string]interface{}:
		return copyMap(v), nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("unsupported MCP input schema type %T: %w", value, err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("unsupported MCP input schema type %T: %w", value, err)
		}
		return out, nil
	}
}

func normalizeMCPOutputSchema(value interface{}) (schema.Schema, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case schema.Schema:
		return v, nil
	case map[string]interface{}:
		return schema.NewSimpleJSONSchema(v), nil
	default:
		return nil, fmt.Errorf("unsupported MCP output schema type %T", value)
	}
}

func extractMCPStructuredOutput(result CallToolResult, outputSchema schema.Schema, toolName string) (interface{}, error) {
	if result.StructuredContent != nil {
		if err := outputSchema.Validator().Validate(result.StructuredContent); err != nil {
			return nil, NewMCPClientError(0, fmt.Sprintf("Tool %q returned structuredContent that does not match the expected outputSchema", toolName), err.Error())
		}
		return schema.ApplyDefaults(result.StructuredContent, outputSchema), nil
	}
	for _, part := range result.Content {
		if part.Type != "text" || !part.hasTextField() {
			continue
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(part.Text), &parsed); err != nil {
			return nil, NewMCPClientError(0, fmt.Sprintf("Tool %q returned content that does not match the expected outputSchema", toolName), err.Error())
		}
		if err := outputSchema.Validator().Validate(parsed); err != nil {
			return nil, NewMCPClientError(0, fmt.Sprintf("Tool %q returned content that does not match the expected outputSchema", toolName), err.Error())
		}
		return schema.ApplyDefaults(parsed, outputSchema), nil
	}
	return nil, NewMCPClientError(0, fmt.Sprintf("Tool %q did not return structuredContent or parseable text content", toolName), nil)
}

// convertToModelOutput converts a tool result to model-readable output
func (c *MCPToolConverter) convertToModelOutput(result interface{}) types.ToolResultOutput {
	mcpResult, ok := result.(CallToolResult)
	if !ok {
		if ptr, ptrOK := result.(*CallToolResult); ptrOK && ptr != nil {
			mcpResult = *ptr
			ok = true
		}
	}
	if !ok || mcpResult.Content == nil {
		return types.ToolResultOutput{
			Type:  types.ToolResultOutputJSON,
			Value: result,
		}
	}

	contentBlocks := make([]types.ToolResultContentBlock, 0, len(mcpResult.Content))
	for _, part := range mcpResult.Content {
		contentBlocks = append(contentBlocks, convertMCPContentToModelOutputBlock(part))
	}

	return types.ToolResultOutput{
		Type:    types.ToolResultOutputContent,
		Content: contentBlocks,
	}
}

func convertMCPContentToModelOutputBlock(part ToolResultContent) types.ToolResultContentBlock {
	if part.Type == "text" && part.hasTextField() {
		return types.TextContentBlock{Text: part.Text}
	}
	if part.Type == "image" && part.hasDataField() && part.hasMimeTypeField() {
		return types.FileContentBlock{
			FileData:  types.FileData{Type: types.FileDataTypeData, DataString: part.Data, MediaType: part.MimeType},
			MediaType: part.MimeType,
		}
	}
	return types.TextContentBlock{Text: marshalMCPContentForModelOutput(part)}
}

func marshalMCPContentForModelOutput(part ToolResultContent) string {
	data, err := json.Marshal(part)
	if err != nil {
		return "{}"
	}
	return string(data)
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

// CreateSSEMCPClient creates an MCP client with legacy SSE transport.
// This is useful for connecting to MCP servers that expose an SSE endpoint and
// POST message endpoint instead of streamable HTTP.
//
// Example:
//
//	client, err := CreateSSEMCPClient("https://mcp.example.com/sse", nil)
//	if err != nil {
//	    // handle error
//	}
//	defer client.Close()
func CreateSSEMCPClient(url string, oauth *OAuthConfig) (*MCPClient, error) {
	transport := NewSSETransport(SSETransportConfig{
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
