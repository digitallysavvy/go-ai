package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMCPServer(t *testing.T) {
	t.Run("creates tool with full config", func(t *testing.T) {
		config := MCPServerConfig{
			ServerURL:     "https://api.example.com/mcp",
			AllowedTools:  []string{"search", "summarize"},
			Authorization: "Bearer test_token",
			Headers: map[string]string{
				"X-API-Version": "v1",
			},
		}

		tool := MCPServer(config)

		// Verify basic properties
		if tool.Name != "xai.mcp" {
			t.Errorf("expected name 'xai.mcp', got %s", tool.Name)
		}

		if tool.Title != "MCP Server" {
			t.Errorf("expected title 'MCP Server', got %s", tool.Title)
		}

		if !tool.ProviderExecuted {
			t.Error("expected ProviderExecuted to be true")
		}

		if tool.Description == "" {
			t.Error("expected non-empty description")
		}

		// Verify parameters structure
		params, ok := tool.Parameters.(map[string]interface{})
		if !ok {
			t.Fatal("expected Parameters to be map[string]interface{}")
		}

		if params["type"] != "object" {
			t.Errorf("expected type 'object', got %v", params["type"])
		}

		properties, ok := params["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties to be map[string]interface{}")
		}

		// Verify required parameters exist
		if _, exists := properties["serverUrl"]; !exists {
			t.Error("expected 'serverUrl' property to exist")
		}

		if _, exists := properties["toolName"]; !exists {
			t.Error("expected 'toolName' property to exist")
		}

		// Verify required fields
		required, ok := params["required"].([]string)
		if !ok {
			t.Fatal("expected required to be []string")
		}

		if len(required) != 2 {
			t.Errorf("expected 2 required fields, got %d", len(required))
		}
	})

	t.Run("creates tool with minimal config", func(t *testing.T) {
		config := MCPServerConfig{
			ServerURL: "https://api.example.com/mcp",
		}

		tool := MCPServer(config)

		if tool.Name != "xai.mcp" {
			t.Errorf("expected name 'xai.mcp', got %s", tool.Name)
		}

		if !tool.ProviderExecuted {
			t.Error("expected ProviderExecuted to be true")
		}
	})

	t.Run("MCPServerSimple helper", func(t *testing.T) {
		serverURL := "https://api.example.com/mcp"
		tool := MCPServerSimple(serverURL)

		if tool.Name != "xai.mcp" {
			t.Errorf("expected name 'xai.mcp', got %s", tool.Name)
		}

		if !tool.ProviderExecuted {
			t.Error("expected ProviderExecuted to be true")
		}

		params := tool.Parameters.(map[string]interface{})
		properties := params["properties"].(map[string]interface{})

		serverUrlParam := properties["serverUrl"].(map[string]interface{})
		if serverUrlParam["default"] != serverURL {
			t.Errorf("expected default serverUrl to be %s, got %v", serverURL, serverUrlParam["default"])
		}
	})

	t.Run("MCPServerWithAuth helper", func(t *testing.T) {
		serverURL := "https://api.example.com/mcp"
		auth := "Bearer test_token"
		tool := MCPServerWithAuth(serverURL, auth)

		if tool.Name != "xai.mcp" {
			t.Errorf("expected name 'xai.mcp', got %s", tool.Name)
		}

		if !tool.ProviderExecuted {
			t.Error("expected ProviderExecuted to be true")
		}

		params := tool.Parameters.(map[string]interface{})
		properties := params["properties"].(map[string]interface{})

		authParam := properties["authorization"].(map[string]interface{})
		if authParam["default"] != auth {
			t.Errorf("expected default authorization to be %s, got %v", auth, authParam["default"])
		}
	})

	t.Run("execute function returns error for provider-executed tool", func(t *testing.T) {
		config := MCPServerConfig{
			ServerURL: "https://api.example.com/mcp",
		}

		tool := MCPServer(config)

		// Execute should return an error since this is a provider-executed tool
		result, err := tool.Execute(nil, map[string]interface{}{
			"serverUrl": "https://api.example.com/mcp",
			"toolName":  "search",
		}, types.ToolExecutionOptions{
			ToolCallID: "test_call_id",
		})

		if result != nil {
			t.Error("expected nil result for provider-executed tool")
		}

		if err == nil {
			t.Error("expected error for provider-executed tool")
		}

		// Verify it's a ToolExecutionError
		if execErr, ok := err.(*types.ToolExecutionError); ok {
			if !execErr.ProviderExecuted {
				t.Error("expected ProviderExecuted to be true in error")
			}
			if execErr.ToolName != "xai.mcp" {
				t.Errorf("expected tool name 'xai.mcp', got %s", execErr.ToolName)
			}
		} else {
			t.Error("expected error to be *types.ToolExecutionError")
		}
	})
}

func TestMCPServer_ParameterSchema(t *testing.T) {
	t.Run("includes allowed tools when provided", func(t *testing.T) {
		config := MCPServerConfig{
			ServerURL:    "https://api.example.com/mcp",
			AllowedTools: []string{"search", "summarize"},
		}

		tool := MCPServer(config)
		params := tool.Parameters.(map[string]interface{})
		properties := params["properties"].(map[string]interface{})

		allowedToolsParam, exists := properties["allowedTools"]
		if !exists {
			t.Error("expected allowedTools property to exist")
		}

		allowedToolsMap := allowedToolsParam.(map[string]interface{})
		if allowedToolsMap["type"] != "array" {
			t.Errorf("expected allowedTools type to be 'array', got %v", allowedToolsMap["type"])
		}
	})

	t.Run("includes headers when provided", func(t *testing.T) {
		config := MCPServerConfig{
			ServerURL: "https://api.example.com/mcp",
			Headers: map[string]string{
				"X-API-Version": "v1",
			},
		}

		tool := MCPServer(config)
		params := tool.Parameters.(map[string]interface{})
		properties := params["properties"].(map[string]interface{})

		headersParam, exists := properties["headers"]
		if !exists {
			t.Error("expected headers property to exist")
		}

		headersMap := headersParam.(map[string]interface{})
		if headersMap["type"] != "object" {
			t.Errorf("expected headers type to be 'object', got %v", headersMap["type"])
		}
	})

	t.Run("includes authorization when provided", func(t *testing.T) {
		config := MCPServerConfig{
			ServerURL:     "https://api.example.com/mcp",
			Authorization: "Bearer test_token",
		}

		tool := MCPServer(config)
		params := tool.Parameters.(map[string]interface{})
		properties := params["properties"].(map[string]interface{})

		authParam, exists := properties["authorization"]
		if !exists {
			t.Error("expected authorization property to exist")
		}

		authMap := authParam.(map[string]interface{})
		if authMap["type"] != "string" {
			t.Errorf("expected authorization type to be 'string', got %v", authMap["type"])
		}
	})

	t.Run("excludes optional parameters when not provided", func(t *testing.T) {
		config := MCPServerConfig{
			ServerURL: "https://api.example.com/mcp",
		}

		tool := MCPServer(config)
		params := tool.Parameters.(map[string]interface{})
		properties := params["properties"].(map[string]interface{})

		// Should not include allowedTools, headers, or authorization
		if _, exists := properties["allowedTools"]; exists {
			t.Error("expected allowedTools property to not exist when not configured")
		}

		if _, exists := properties["headers"]; exists {
			t.Error("expected headers property to not exist when not configured")
		}

		if _, exists := properties["authorization"]; exists {
			t.Error("expected authorization property to not exist when not configured")
		}
	})
}
