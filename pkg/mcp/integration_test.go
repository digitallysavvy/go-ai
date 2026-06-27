package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func TestMCPToolConverterPropagatesMcpProviderMetadata(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	client.serverInfo = ServerInfo{Name: "filesystem", Version: "1.0.0"}

	converter := NewMCPToolConverter(client)
	tool, err := converter.convertTool(MCPTool{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	}, nil)
	if err != nil {
		t.Fatalf("convertTool error: %v", err)
	}

	if tool.ProviderName != "mcp" {
		t.Fatalf("ProviderName = %q, want mcp", tool.ProviderName)
	}
	mcpMeta, ok := tool.ProviderMetadata["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing mcp provider metadata: %#v", tool.ProviderMetadata)
	}
	if mcpMeta["clientName"] != "ai-sdk-mcp-client" || mcpMeta["toolName"] != "read_file" {
		t.Fatalf("mcp metadata = %#v", mcpMeta)
	}
	if _, ok := mcpMeta["serverName"]; ok {
		t.Fatalf("serverName should not be present in TS-parity McpProviderMetadata: %#v", mcpMeta)
	}
	if tool.Metadata["toolName"] != "read_file" {
		t.Fatalf("tool metadata = %#v, want toolName=read_file", tool.Metadata)
	}
	if tool.Type != types.ToolTypeDynamic {
		t.Fatalf("automatic MCP tool Type = %q, want dynamic", tool.Type)
	}
}

func TestMCPToolConverterTitleUsesTSNullishSemantics(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	var mcpTool MCPTool
	if err := json.Unmarshal([]byte(`{
		"name": "empty-title",
		"title": "",
		"description": "tool",
		"inputSchema": {"type": "object"},
		"annotations": {"title": "Annotation title"}
	}`), &mcpTool); err != nil {
		t.Fatalf("unmarshal MCP tool: %v", err)
	}
	tool, err := converter.convertTool(mcpTool, nil)
	if err != nil {
		t.Fatalf("convertTool error: %v", err)
	}
	if tool.Title != "" {
		t.Fatalf("Tool title = %q, want explicit empty title", tool.Title)
	}
	if title, ok := tool.Metadata["title"].(string); !ok || title != "" {
		t.Fatalf("MCP metadata title = %#v, want explicit empty string", tool.Metadata["title"])
	}
}

func TestMCPToolConverterModelOutputFallbackAndFactoryHelpers(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	out := converter.convertToModelOutput(map[string]interface{}{"a": 1})
	if out.Type != types.ToolResultOutputJSON {
		t.Fatalf("fallback output type = %q, want json", out.Type)
	}
	if value, ok := out.Value.(map[string]interface{}); !ok || value["a"] != 1 {
		t.Fatalf("fallback output value = %#v, want raw JSON value", out.Value)
	}

	contentOut := converter.convertToModelOutput(CallToolResult{
		Content: []ToolResultContent{
			{Type: "text", Text: "hello"},
			{Type: "image", Data: "AQI=", MimeType: "image/png"},
			{Type: "resource_link", URI: "file://x.bin", Name: "x.bin"},
		},
	})
	if contentOut.Type != types.ToolResultOutputContent || len(contentOut.Content) != 3 {
		t.Fatalf("content output = %#v", contentOut)
	}
	fileBlock, ok := contentOut.Content[1].(types.FileContentBlock)
	if !ok {
		t.Fatalf("image MCP content mapped to %T, want FileContentBlock", contentOut.Content[1])
	}
	if fileBlock.FileData.Type != types.FileDataTypeData || fileBlock.FileData.MediaType != "image/png" || fileBlock.FileData.DataString != "AQI=" {
		t.Fatalf("image MCP content file data = %+v, want data file block", fileBlock.FileData)
	}
	opaqueBlock, ok := contentOut.Content[2].(types.TextContentBlock)
	if !ok || opaqueBlock.Text != `{"type":"resource_link","uri":"file://x.bin","name":"x.bin"}` {
		t.Fatalf("opaque MCP content block = %#v, want JSON text block", contentOut.Content[2])
	}

	mcpClient, err := CreateMCPClient(MCPClientConfig{}, newMockTransport())
	if err != nil || mcpClient == nil {
		t.Fatalf("CreateMCPClient() = (%v, %v)", mcpClient, err)
	}

	stdioClient, err := CreateStdioMCPClient("cat", nil)
	if err != nil || stdioClient == nil {
		t.Fatalf("CreateStdioMCPClient() = (%v, %v)", stdioClient, err)
	}
	httpClient, err := CreateHTTPMCPClient("https://example.com/mcp", nil)
	if err != nil || httpClient == nil {
		t.Fatalf("CreateHTTPMCPClient() = (%v, %v)", httpClient, err)
	}
}

func TestMCPToolConverterExecuteReturnsRawCallToolResult(t *testing.T) {
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close()
	converter := NewMCPToolConverter(client)

	tool, err := converter.convertTool(MCPTool{
		Name:        "test-tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
	}, nil)
	if err != nil {
		t.Fatalf("convertTool error: %v", err)
	}

	result, err := tool.Execute(context.Background(), map[string]interface{}{"input": "x"}, types.ToolExecutionOptions{})
	if err != nil {
		t.Fatalf("Execute error = %v", err)
	}
	callResult, ok := result.(*CallToolResult)
	if !ok {
		t.Fatalf("Execute result = %T, want *CallToolResult", result)
	}
	if len(callResult.Content) != 1 || callResult.Content[0].Text != "tool-ok" {
		t.Fatalf("Execute result = %#v, want raw MCP CallToolResult", callResult)
	}
}

func TestMCPToolConverterModelOutputUsesTSPropertyPresenceChecks(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	var missingData CallToolResult
	if err := json.Unmarshal([]byte(`{"content":[{"type":"image","mimeType":"image/png"}]}`), &missingData); err != nil {
		t.Fatalf("unmarshal missing data: %v", err)
	}
	out := converter.convertToModelOutput(missingData)
	if out.Type != types.ToolResultOutputContent || len(out.Content) != 1 {
		t.Fatalf("missing-data output = %#v", out)
	}
	textBlock, ok := out.Content[0].(types.TextContentBlock)
	if !ok || textBlock.Text != `{"type":"image","mimeType":"image/png"}` {
		t.Fatalf("missing-data image block = %#v, want JSON text fallback", out.Content[0])
	}

	var emptyData CallToolResult
	if err := json.Unmarshal([]byte(`{"content":[{"type":"image","data":"","mimeType":"image/png"}]}`), &emptyData); err != nil {
		t.Fatalf("unmarshal empty data: %v", err)
	}
	out = converter.convertToModelOutput(emptyData)
	fileBlock, ok := out.Content[0].(types.FileContentBlock)
	if !ok {
		t.Fatalf("empty-data image block = %T, want FileContentBlock", out.Content[0])
	}
	if fileBlock.FileData.Type != types.FileDataTypeData || fileBlock.FileData.DataString != "" || fileBlock.FileData.MediaType != "image/png" {
		t.Fatalf("empty-data image file data = %+v, want present empty data", fileBlock.FileData)
	}

	var custom CallToolResult
	if err := json.Unmarshal([]byte(`{"content":[{"type":"custom","data":{"foo":"bar"},"audience":["assistant"]}]}`), &custom); err != nil {
		t.Fatalf("unmarshal custom content: %v", err)
	}
	out = converter.convertToModelOutput(custom)
	customBlock, ok := out.Content[0].(types.TextContentBlock)
	if !ok || customBlock.Text != `{"type":"custom","data":{"foo":"bar"},"audience":["assistant"]}` {
		t.Fatalf("custom content block = %#v, want JSON text preserving unknown fields", out.Content[0])
	}
}

func TestMCPToolConverterAutomaticSchemaAddsPropertiesAndDisallowsAdditional(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	tools, err := converter.ToolsFromDefinitions([]MCPTool{{
		Name:        "auto",
		Description: "automatic schema",
		InputSchema: map[string]interface{}{"type": "object", "properties": nil},
	}}, nil)
	if err != nil {
		t.Fatalf("ToolsFromDefinitions error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}
	params, ok := tools[0].Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("Parameters = %T, want map", tools[0].Parameters)
	}
	if _, ok := params["properties"].(map[string]interface{}); !ok {
		t.Fatalf("Parameters properties = %#v, want empty object", params["properties"])
	}
	if params["additionalProperties"] != false {
		t.Fatalf("Parameters additionalProperties = %#v, want false", params["additionalProperties"])
	}
}

func TestMCPToolConverterSchemaProvidedToolsAreStaticAndExposeRawMeta(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	tools, err := converter.ToolsFromDefinitions([]MCPTool{{
		Name:        "with-meta",
		Description: "tool with meta",
		InputSchema: map[string]interface{}{"type": "object"},
		Meta: map[string]interface{}{
			"openai/outputTemplate": "{{result}}",
		},
	}}, map[string]MCPToolSchema{
		"with-meta": {InputSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})},
	})
	if err != nil {
		t.Fatalf("ToolsFromDefinitions error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}
	if tools[0].Type != types.ToolTypeFunction {
		t.Fatalf("schema-provided MCP tool Type = %q, want function", tools[0].Type)
	}
	if tools[0].Meta["openai/outputTemplate"] != "{{result}}" {
		t.Fatalf("raw MCP _meta = %#v", tools[0].Meta)
	}
	if tools[0].Metadata["toolName"] != "with-meta" {
		t.Fatalf("MCP metadata = %#v, want generated metadata preserved", tools[0].Metadata)
	}
	params, ok := tools[0].Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("Parameters = %T, want JSON Schema map", tools[0].Parameters)
	}
	if params["type"] != "object" {
		t.Fatalf("Parameters = %#v, want normalized JSON Schema map", params)
	}
}

func TestMCPToolConverterSchemasFilterAndValidateOutput(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	outputSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"temperature", "conditions"},
		"properties": map[string]interface{}{
			"temperature": map[string]interface{}{"type": "number"},
			"conditions":  map[string]interface{}{"type": "string"},
		},
	})
	tools, err := converter.ToolsFromDefinitions([]MCPTool{
		{Name: "weather-tool", Description: "weather", InputSchema: map[string]interface{}{"type": "object"}},
		{Name: "ignored-tool", Description: "ignored", InputSchema: map[string]interface{}{"type": "object"}},
	}, map[string]MCPToolSchema{
		"weather-tool": {
			InputSchema:  schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
			OutputSchema: outputSchema,
		},
	})
	if err != nil {
		t.Fatalf("ToolsFromDefinitions error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "weather-tool" {
		t.Fatalf("tools = %+v, want only weather-tool", tools)
	}
	if tools[0].OutputSchema != outputSchema {
		t.Fatalf("OutputSchema = %#v, want provided schema", tools[0].OutputSchema)
	}
	result, err := extractMCPStructuredOutput(CallToolResult{
		StructuredContent: map[string]interface{}{
			"temperature": 22.5,
			"conditions":  "Sunny",
		},
	}, outputSchema, "weather-tool")
	if err != nil {
		t.Fatalf("extract structured content error: %v", err)
	}
	got := result.(map[string]interface{})
	if got["temperature"] != 22.5 || got["conditions"] != "Sunny" {
		t.Fatalf("structured output = %#v", result)
	}
}

func TestMCPToolConverterOutputSchemaParsesTextAndBypassesErrors(t *testing.T) {
	outputSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"value", "name"},
		"properties": map[string]interface{}{
			"value": map[string]interface{}{"type": "number"},
			"name":  map[string]interface{}{"type": "string"},
		},
	})
	result, err := extractMCPStructuredOutput(CallToolResult{
		Content: []ToolResultContent{{Type: "text", Text: `{"value":42,"name":"test"}`}},
	}, outputSchema, "json-tool")
	if err != nil {
		t.Fatalf("extract text content error: %v", err)
	}
	got := result.(map[string]interface{})
	if got["value"] != float64(42) || got["name"] != "test" {
		t.Fatalf("text output = %#v", result)
	}

	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)
	tool, err := converter.convertTool(MCPTool{
		Name:        "error-tool",
		Description: "errors",
		InputSchema: map[string]interface{}{"type": "object"},
	}, &MCPToolSchema{OutputSchema: outputSchema})
	if err != nil {
		t.Fatalf("convertTool error: %v", err)
	}
	if tool.OutputSchema == nil {
		t.Fatal("OutputSchema should be set")
	}
	raw := &CallToolResult{IsError: true, StructuredContent: map[string]interface{}{"invalid": true}}
	modelOut, err := tool.ToModelOutput(context.Background(), types.ToModelOutputOptions{Output: raw})
	if err != nil {
		t.Fatalf("ToModelOutput error: %v", err)
	}
	if modelOut.Type != types.ToolResultOutputJSON || modelOut.Value != raw {
		t.Fatalf("error result model output = %#v, want raw json output without validation", modelOut)
	}
}

func TestMCPToolConverterInvalidAppMetadataReturnsError(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)

	_, err := converter.convertTool(MCPTool{
		Name:        "bad_app",
		InputSchema: map[string]interface{}{"type": "object"},
		Meta: map[string]interface{}{"ui": map[string]interface{}{
			"resourceUri": "https://example.com/app.html",
		}},
	}, nil)
	if err == nil {
		t.Fatal("expected invalid MCP App resource URI error")
	}
}

type listToolErrorClient struct{}

func (listToolErrorClient) ListTools(context.Context) ([]MCPTool, error) {
	return nil, errors.New("boom")
}

func TestConvertToGoAIToolsErrorPath(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	client.initialized = true
	converter := NewMCPToolConverter(client)

	// Force conversion execution path by setting a non-empty tool and calling Execute.
	client.serverInfo = ServerInfo{Name: "server"}
	tool, err := converter.convertTool(MCPTool{
		Name:        "echo",
		Description: "echo",
		InputSchema: map[string]interface{}{"type": "object"},
	}, nil)
	if err != nil {
		t.Fatalf("convertTool error: %v", err)
	}

	_, err = tool.Execute(context.Background(), map[string]interface{}{}, types.ToolExecutionOptions{})
	if err == nil {
		t.Fatal("expected Execute to fail when mock transport isn't connected")
	}

	if _, err := GetMCPToolsForAgent(context.Background(), client); err == nil {
		// A disconnected mock client should fail ListTools and cover error wrapping path.
		t.Fatal("GetMCPToolsForAgent() expected error on disconnected client")
	}
}
