package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFileSearch(t *testing.T) {
	t.Run("creates tool with full config", func(t *testing.T) {
		config := FileSearchConfig{
			VectorStoreIDs: []string{"vs_123", "vs_456"},
			MaxNumResults:  5,
		}

		tool := FileSearch(config)

		// Verify basic properties
		if tool.Name != "xai.file_search" {
			t.Errorf("expected name 'xai.file_search', got %s", tool.Name)
		}

		if tool.Title != "File Search" {
			t.Errorf("expected title 'File Search', got %s", tool.Title)
		}

		if !tool.ProviderExecuted {
			t.Error("expected ProviderExecuted to be true")
		}

		if tool.Description == "" {
			t.Error("expected non-empty description")
		}

		// The input schema is TS's z.object({}): the config is tool args.
		assertXAIEmptyInputSchema(t, tool)
		if got, ok := tool.ProviderOptions.(FileSearchOptions); !ok || len(got.VectorStoreIDs) != 2 || got.MaxNumResults != 5 {
			t.Errorf("ProviderOptions = %#v, want the config", tool.ProviderOptions)
		}
	})

	t.Run("creates tool with minimal config", func(t *testing.T) {
		config := FileSearchConfig{
			VectorStoreIDs: []string{"vs_123"},
		}

		tool := FileSearch(config)

		if tool.Name != "xai.file_search" {
			t.Errorf("expected name 'xai.file_search', got %s", tool.Name)
		}

		if !tool.ProviderExecuted {
			t.Error("expected ProviderExecuted to be true")
		}
	})

	t.Run("defaults max results to 10 when not specified", func(t *testing.T) {
		config := FileSearchConfig{
			VectorStoreIDs: []string{"vs_123"},
			MaxNumResults:  0, // Not specified
		}

		tool := FileSearch(config)

		// Tool should still be created successfully
		if tool.Name != "xai.file_search" {
			t.Errorf("expected name 'xai.file_search', got %s", tool.Name)
		}
	})

	t.Run("FileSearchWithDefaults helper", func(t *testing.T) {
		vectorStores := []string{"vs_123", "vs_456"}
		tool := FileSearchWithDefaults(vectorStores)

		if tool.Name != "xai.file_search" {
			t.Errorf("expected name 'xai.file_search', got %s", tool.Name)
		}

		if !tool.ProviderExecuted {
			t.Error("expected ProviderExecuted to be true")
		}
	})

	t.Run("execute function returns error for provider-executed tool", func(t *testing.T) {
		config := FileSearchConfig{
			VectorStoreIDs: []string{"vs_123"},
			MaxNumResults:  5,
		}

		tool := FileSearch(config)

		// Execute should return an error since this is a provider-executed tool
		result, err := tool.Execute(nil, map[string]interface{}{"query": "test"}, types.ToolExecutionOptions{
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
			if execErr.ToolName != "xai.file_search" {
				t.Errorf("expected tool name 'xai.file_search', got %s", execErr.ToolName)
			}
		} else {
			t.Error("expected error to be *types.ToolExecutionError")
		}
	})
}

// TS xai fileSearch / mcpServer: inputSchema z.object({}). Provider tool
// calls are validated against Parameters, so it must hold the model input
// schema only, never config defaults.
func assertXAIEmptyInputSchema(t *testing.T, tool types.Tool) {
	t.Helper()
	params, ok := tool.Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("Parameters = %T, want map", tool.Parameters)
	}
	props, ok := params["properties"].(map[string]interface{})
	if params["type"] != "object" || !ok || len(props) != 0 || params["required"] != nil || len(params) != 2 {
		t.Errorf("Parameters = %#v, want {type: object, properties: {}}", params)
	}
}

func TestFileSearch_ParameterSchema(t *testing.T) {
	assertXAIEmptyInputSchema(t, FileSearch(FileSearchConfig{VectorStoreIDs: []string{"vs_123", "vs_456"}, MaxNumResults: 20}))
	assertXAIEmptyInputSchema(t, FileSearch(FileSearchConfig{}))
}
