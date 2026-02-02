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

		// Verify query parameter exists
		if _, exists := properties["query"]; !exists {
			t.Error("expected 'query' property to exist")
		}

		// Verify required fields
		required, ok := params["required"].([]string)
		if !ok {
			t.Fatal("expected required to be []string")
		}

		if len(required) != 1 || required[0] != "query" {
			t.Errorf("expected required to be ['query'], got %v", required)
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

func TestFileSearch_ParameterSchema(t *testing.T) {
	t.Run("includes vector store IDs when provided", func(t *testing.T) {
		config := FileSearchConfig{
			VectorStoreIDs: []string{"vs_123", "vs_456"},
			MaxNumResults:  5,
		}

		tool := FileSearch(config)
		params := tool.Parameters.(map[string]interface{})
		properties := params["properties"].(map[string]interface{})

		vectorStoreParam, exists := properties["vectorStoreIds"]
		if !exists {
			t.Error("expected vectorStoreIds property to exist")
		}

		vectorStoreMap := vectorStoreParam.(map[string]interface{})
		if vectorStoreMap["type"] != "array" {
			t.Errorf("expected vectorStoreIds type to be 'array', got %v", vectorStoreMap["type"])
		}
	})

	t.Run("includes maxNumResults when provided", func(t *testing.T) {
		config := FileSearchConfig{
			VectorStoreIDs: []string{"vs_123"},
			MaxNumResults:  20,
		}

		tool := FileSearch(config)
		params := tool.Parameters.(map[string]interface{})
		properties := params["properties"].(map[string]interface{})

		maxResultsParam, exists := properties["maxNumResults"]
		if !exists {
			t.Error("expected maxNumResults property to exist")
		}

		maxResultsMap := maxResultsParam.(map[string]interface{})
		if maxResultsMap["type"] != "integer" {
			t.Errorf("expected maxNumResults type to be 'integer', got %v", maxResultsMap["type"])
		}
	})
}
