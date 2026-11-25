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

		assertXAILooseInputSchema(t, tool)
		if got, ok := tool.ProviderOptions.(MCPServerOptions); !ok || got.ServerURL != config.ServerURL || got.Authorization != config.Authorization {
			t.Errorf("ProviderOptions = %#v, want the config", tool.ProviderOptions)
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

		assertXAILooseInputSchema(t, tool)
		if got, ok := tool.ProviderOptions.(MCPServerOptions); !ok || got.ServerURL != serverURL {
			t.Errorf("ProviderOptions = %#v, want server URL %s", tool.ProviderOptions, serverURL)
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

		// The token stays in the tool args, never in the input schema.
		assertXAILooseInputSchema(t, tool)
		if got, ok := tool.ProviderOptions.(MCPServerOptions); !ok || got.Authorization != auth {
			t.Errorf("ProviderOptions = %#v, want authorization", tool.ProviderOptions)
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
	assertXAILooseInputSchema(t, MCPServer(MCPServerConfig{
		ServerURL:     "https://api.example.com/mcp",
		AllowedTools:  []string{"search"},
		Authorization: "Bearer test_token",
		Headers:       map[string]string{"X-API-Version": "v1"},
	}))
	assertXAILooseInputSchema(t, MCPServer(MCPServerConfig{ServerURL: "https://api.example.com/mcp"}))
}

// assertXAILooseInputSchema checks z.looseObject({}) (TS 6f56c29ff6 mcp,
// 61ce23efab x_search): the model's tool-call arguments stay in the input.
func assertXAILooseInputSchema(t *testing.T, tool types.Tool) {
	t.Helper()
	params, ok := tool.Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("Parameters = %T, want map", tool.Parameters)
	}
	props, ok := params["properties"].(map[string]interface{})
	extra, hasExtra := params["additionalProperties"].(map[string]interface{})
	if params["type"] != "object" || !ok || len(props) != 0 || !hasExtra || len(extra) != 0 || params["required"] != nil || len(params) != 3 {
		t.Errorf("Parameters = %#v, want {type: object, properties: {}, additionalProperties: {}}", params)
	}
}
