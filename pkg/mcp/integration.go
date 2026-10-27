package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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

// ConvertToGoAITools fetches MCP tools and converts them to Go-AI tools.
// Fetches every page of tools/list (following NextCursor), matching
// TypeScript's client.tools() (mcp-client.ts, hash 1175434).
func (c *MCPToolConverter) ConvertToGoAITools(ctx context.Context) ([]types.Tool, error) {
	mcpTools, err := c.client.ListAllTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP tools: %w", err)
	}

	return c.ToolsFromDefinitions(mcpTools, nil)
}

// ConvertToGoAIToolsWithSchemas fetches MCP tools and applies a schemas map,
// matching TypeScript client.tools({ schemas }). Tools not present in schemas
// are omitted, and per-tool input/output schemas override discovered schemas.
// Fetches every page of tools/list (following NextCursor), matching
// TypeScript's client.tools() (mcp-client.ts, hash 1175434).
func (c *MCPToolConverter) ConvertToGoAIToolsWithSchemas(ctx context.Context, schemas map[string]MCPToolSchema) ([]types.Tool, error) {
	mcpTools, err := c.client.ListAllTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP tools: %w", err)
	}
	return c.ToolsFromDefinitions(mcpTools, schemas)
}

// ToolsFromDefinitions converts already-fetched MCP tool definitions, matching
// TypeScript toolsFromDefinitions. Passing nil schemas uses automatic schema
// discovery; a non-nil schemas map filters to own matching tool names.
func (c *MCPToolConverter) ToolsFromDefinitions(mcpTools []MCPTool, schemas map[string]MCPToolSchema) ([]types.Tool, error) {
	// TS toolsFromDefinitions runs prepareToolDefinitions, so definitions
	// fetched earlier (e.g. cached via GetSerializableTools) still register
	// their x-mcp-header bindings and drop tools with invalid annotations.
	prepared := ListToolsResult{Tools: append([]MCPTool(nil), mcpTools...)}
	c.client.prepareToolDefinitions(&prepared, false)
	mcpTools = prepared.Tools

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
	if annotations := extractMCPToolAnnotations(mcpTool.Annotations); annotations != nil {
		mcpMetadata["annotations"] = annotations
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
	parameters, parameterKeyOrder := automaticMCPParameters(mcpTool)
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
			parameterKeyOrder = nil
		}
		var err error
		outputSchema, err = normalizeMCPOutputSchema(toolSchema.OutputSchema)
		if err != nil {
			return types.Tool{}, err
		}
	}

	return types.Tool{
		Name:              mcpTool.Name,
		Description:       mcpTool.Description,
		Title:             resolvedTitle,
		Parameters:        parameters,
		ParameterKeyOrder: parameterKeyOrder,
		OutputSchema:      outputSchema,
		ProviderName:      "mcp",
		ProviderMetadata:  providerMetadata,
		Metadata:          mcpMetadata,
		Meta:              copyMap(mcpTool.Meta),
		Type:              toolType,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			// Call MCP tool
			result, err := c.client.CallTool(ctx, mcpTool.Name, input)
			if err != nil {
				return nil, fmt.Errorf("MCP tool execution failed: %w", err)
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

func resolveMCPToolTitle(tool MCPTool) (string, bool) {
	if tool.titlePresent || tool.Title != "" {
		return tool.Title, true
	}
	if annotationsTitle, ok := tool.Annotations["title"].(string); ok {
		return annotationsTitle, true
	}
	return "", false
}

// extractMCPToolAnnotations surfaces the known McpToolAnnotations hint keys
// from a raw MCP tool's annotations object, matching TS toolsFromDefinitions
// (mcp-client.ts). Unknown keys are dropped. Returns nil when annotations is
// nil (the "annotations" property was absent from the tool definition);
// returns a (possibly empty) map otherwise, matching TS's `annotations != null`
// check, which still emits an "annotations" key when the object carries none
// of the five known hints.
func extractMCPToolAnnotations(annotations map[string]interface{}) map[string]interface{} {
	if annotations == nil {
		return nil
	}
	out := map[string]interface{}{}
	if title, ok := annotations["title"].(string); ok {
		out["title"] = title
	}
	for _, key := range []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"} {
		if value, ok := annotations[key].(bool); ok {
			out[key] = value
		}
	}
	return out
}

// automaticMCPParameters is the tool's Parameters for a tool converted with no
// explicit schema: the server's inputSchema, normalized, as a plain map. The
// second result is the key order the server sent, for Tool.ParameterKeyOrder,
// as TS spreads the JS object
// `{...inputSchema, properties: inputSchema.properties ?? {},
// additionalProperties: false}`: the server's keys in order, then
// `properties` and `additionalProperties` if the server did not send them.
func automaticMCPParameters(mcpTool MCPTool) (map[string]interface{}, interface{}) {
	normalized := normalizeAutomaticMCPInputSchema(mcpTool.InputSchema)
	hint, ok := mcpTool.inputSchemaHint.(*jsonorder.Object)
	if !ok {
		return normalized, nil
	}
	ordered := jsonorder.New()
	for _, k := range hint.Keys() {
		v, _ := hint.Get(k)
		ordered.Set(k, v)
	}
	for _, k := range []string{"properties", "additionalProperties"} {
		if _, has := ordered.Get(k); !has {
			ordered.Set(k, nil)
		}
	}
	return normalized, ordered
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

// extractMCPStructuredOutput validates a tool result's structuredContent (or,
// failing that, its first parseable text content) against outputSchema and
// returns the validated value. Mirrors TS mcp-client.ts's
// extractStructuredContent, which validates through safeValidateTypes /
// safeParseJSON -- zod/standard-schema's parse step, which fills any
// .default() values as part of parsing itself and returns that defaulted
// value. A field missing from the tool's result but declared with a schema
// default must therefore not fail validation, so defaults are applied
// before validating (not after).
func extractMCPStructuredOutput(result CallToolResult, outputSchema schema.Schema, toolName string) (interface{}, error) {
	if result.StructuredContent != nil {
		defaulted := schema.ApplyDefaults(result.StructuredContent, outputSchema)
		if err := outputSchema.Validator().Validate(defaulted); err != nil {
			return nil, &MCPClientError{Message: "Tool \"" + toolName + "\" returned structuredContent that does not match the expected outputSchema", Cause: err}
		}
		return defaulted, nil
	}
	// TS uses the first text part (content.find); one without a text field
	// falls through to the error below.
	for _, part := range result.Content {
		if part.Type != "text" {
			continue
		}
		if !part.hasTextField() {
			break
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(part.Text), &parsed); err != nil {
			return nil, &MCPClientError{Message: "Tool \"" + toolName + "\" returned content that does not match the expected outputSchema", Cause: err}
		}
		defaulted := schema.ApplyDefaults(parsed, outputSchema)
		if err := outputSchema.Validator().Validate(defaulted); err != nil {
			return nil, &MCPClientError{Message: "Tool \"" + toolName + "\" returned content that does not match the expected outputSchema", Cause: err}
		}
		return defaulted, nil
	}
	return nil, NewMCPClientError(0, "Tool \""+toolName+"\" did not return structuredContent or parseable text content", nil)
}

// convertToModelOutput converts a tool result to model-readable output
//
// Primitive and nullish outputs (for example from an outputSchema or an
// application wrapper that serializes the result) are handled before the
// MCP envelope check: a string is text, anything else without MCP content is
// JSON, with nil staying a JSON null (TS #23935).
func (c *MCPToolConverter) convertToModelOutput(result interface{}) types.ToolResultOutput {
	if text, ok := result.(string); ok {
		return types.ToolResultOutput{Type: types.ToolResultOutputText, Value: text}
	}
	mcpResult, ok := result.(CallToolResult)
	if !ok {
		if ptr, ptrOK := result.(*CallToolResult); ptrOK && ptr != nil {
			mcpResult = *ptr
			ok = true
		}
	}
	if !ok {
		// TS converts any object with a content array, not only a typed
		// CallToolResult (for example a map from an application wrapper).
		if blocks, isContent := genericMCPContentBlocks(result); isContent {
			return types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: blocks}
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

// genericMCPContentBlocks mirrors the TS duck-typed check
// `'content' in output && Array.isArray(output.content)` for values that are
// not a typed CallToolResult: maps and structs (or pointers to them) whose
// JSON form is an object with a "content" array. Each part is converted with
// the same rules as typed content; unknown parts keep their JSON text.
func genericMCPContentBlocks(result interface{}) ([]types.ToolResultContentBlock, bool) {
	rv := reflect.ValueOf(result)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || (rv.Kind() != reflect.Map && rv.Kind() != reflect.Struct) {
		return nil, false
	}
	data, err := jsonstr.Marshal(result)
	if err != nil {
		return nil, false
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, false
	}
	var parts []json.RawMessage
	rawContent, has := top["content"]
	if !has || len(bytes.TrimSpace(rawContent)) == 0 || bytes.TrimSpace(rawContent)[0] != '[' {
		return nil, false
	}
	if err := json.Unmarshal(rawContent, &parts); err != nil {
		return nil, false
	}
	blocks := make([]types.ToolResultContentBlock, 0, len(parts))
	for _, raw := range parts {
		blocks = append(blocks, convertGenericMCPContentPart(raw))
	}
	return blocks, true
}

func convertGenericMCPContentPart(raw json.RawMessage) types.ToolResultContentBlock {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err == nil && fields != nil {
		var partType string
		_ = json.Unmarshal(fields["type"], &partType)
		if text, has := fields["text"]; has && partType == "text" {
			return types.TextContentBlock{Text: rawJSONStringValue(text)}
		}
		data, hasData := fields["data"]
		mime, hasMime := fields["mimeType"]
		if partType == "image" && hasData && hasMime {
			mediaType := rawJSONStringValue(mime)
			return types.FileContentBlock{
				FileData:  types.FileData{Type: types.FileDataTypeData, DataString: rawJSONStringValue(data), MediaType: mediaType},
				MediaType: mediaType,
			}
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return types.TextContentBlock{Text: string(raw)}
	}
	return types.TextContentBlock{Text: compact.String()}
}

// rawJSONStringValue returns a JSON string's value, or the compact JSON text
// of any other value (TS casts with `as string` and passes it through).
func rawJSONStringValue(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return string(raw)
	}
	return compact.String()
}

func marshalMCPContentForModelOutput(part ToolResultContent) string {
	data, err := jsonstr.Marshal(part)
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
		ClientName:    "go-ai-mcp-client",
		ClientVersion: "1.0.0",
		EnableLogging: false,
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
		ClientName:    "go-ai-mcp-client",
		ClientVersion: "1.0.0",
		EnableLogging: false,
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
		ClientName:    "go-ai-mcp-client",
		ClientVersion: "1.0.0",
		EnableLogging: false,
	}

	return CreateMCPClient(config, transport)
}
