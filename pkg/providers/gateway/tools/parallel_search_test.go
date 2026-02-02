package tools

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestNewParallelSearch(t *testing.T) {
	tests := []struct {
		name   string
		config ParallelSearchConfig
	}{
		{
			name: "default config",
			config: ParallelSearchConfig{},
		},
		{
			name: "one-shot mode",
			config: ParallelSearchConfig{
				Mode:       "one-shot",
				MaxResults: 10,
			},
		},
		{
			name: "agentic mode with source policy",
			config: ParallelSearchConfig{
				Mode:       "agentic",
				MaxResults: 5,
				SourcePolicy: &ParallelSearchSourcePolicy{
					IncludeDomains: []string{"wikipedia.org", "nature.com"},
					AfterDate:      "2024-01-01",
				},
			},
		},
		{
			name: "with excerpts and fetch policy",
			config: ParallelSearchConfig{
				Excerpts: &ParallelSearchExcerpts{
					MaxCharsPerResult: 500,
					MaxCharsTotal:     5000,
				},
				FetchPolicy: &ParallelSearchFetchPolicy{
					MaxAgeSeconds: 3600,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewParallelSearch(tt.config)

			// Convert to types.Tool
			typesTool := tool.ToTool()

			// Check tool name
			if typesTool.Name != "gateway.parallel_search" {
				t.Errorf("Expected tool name 'gateway.parallel_search', got %s", typesTool.Name)
			}

			// Check provider executed flag
			if !typesTool.ProviderExecuted {
				t.Error("Expected ProviderExecuted to be true")
			}

			// Check parameters exist
			if typesTool.Parameters == nil {
				t.Error("Expected Parameters to be set")
			}

			// Verify parameters structure
			params, ok := typesTool.Parameters.(map[string]interface{})
			if !ok {
				t.Fatal("Parameters should be map[string]interface{}")
			}

			properties, ok := params["properties"].(map[string]interface{})
			if !ok {
				t.Fatal("Parameters should have properties field")
			}

			// Check required field exists
			if _, ok := properties["objective"]; !ok {
				t.Error("Expected 'objective' field in properties")
			}

			// Check required fields
			required, ok := params["required"].([]string)
			if !ok {
				t.Fatal("Parameters should have required field")
			}

			if len(required) != 1 || required[0] != "objective" {
				t.Errorf("Expected required to be ['objective'], got %v", required)
			}
		})
	}
}

func TestParallelSearchTool_ToTool(t *testing.T) {
	config := ParallelSearchConfig{
		Mode:       "one-shot",
		MaxResults: 10,
	}

	tool := NewParallelSearch(config)
	typesTool := tool.ToTool()

	// Verify it's a valid types.Tool
	if typesTool.Name == "" {
		t.Error("Expected non-empty tool name")
	}

	if typesTool.Description == "" {
		t.Error("Expected non-empty description")
	}

	if typesTool.Execute == nil {
		t.Error("Expected Execute function to be set")
	}
}

func TestParallelSearch_ProviderExecuted(t *testing.T) {
	tool := NewParallelSearch(ParallelSearchConfig{})
	typesTool := tool.ToTool()

	// Execute should return an error for provider-executed tools
	result, err := typesTool.Execute(nil, nil, types.ToolExecutionOptions{})
	if err == nil {
		t.Error("Expected error when executing provider-executed tool")
	}

	if result != nil {
		t.Errorf("Expected nil result, got %v", result)
	}

	// Check error is ToolExecutionError
	toolErr, ok := err.(*types.ToolExecutionError)
	if !ok {
		t.Errorf("Expected ToolExecutionError, got %T", err)
	}

	if !toolErr.ProviderExecuted {
		t.Error("Expected ProviderExecuted to be true in error")
	}
}
