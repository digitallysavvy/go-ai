package tools

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestNewPerplexitySearch(t *testing.T) {
	tests := []struct {
		name   string
		config PerplexitySearchConfig
	}{
		{
			name:   "default config",
			config: PerplexitySearchConfig{},
		},
		{
			name: "with basic filters",
			config: PerplexitySearchConfig{
				MaxResults:       10,
				MaxTokensPerPage: 2048,
				Country:          "US",
			},
		},
		{
			name: "with domain and language filters",
			config: PerplexitySearchConfig{
				SearchDomainFilter:   []string{"nature.com", "science.org"},
				SearchLanguageFilter: []string{"en", "fr"},
			},
		},
		{
			name: "with recency filter",
			config: PerplexitySearchConfig{
				SearchRecencyFilter: "week",
			},
		},
		{
			name: "all options",
			config: PerplexitySearchConfig{
				MaxResults:           20,
				MaxTokensPerPage:     1024,
				MaxTokens:            10000,
				Country:              "GB",
				SearchDomainFilter:   []string{"example.com"},
				SearchLanguageFilter: []string{"en"},
				SearchRecencyFilter:  "month",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewPerplexitySearch(tt.config)

			// Convert to types.Tool
			typesTool := tool.ToTool()

			// Check tool name
			if typesTool.Name != "gateway.perplexity_search" {
				t.Errorf("Expected tool name 'gateway.perplexity_search', got %s", typesTool.Name)
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
			if _, ok := properties["query"]; !ok {
				t.Error("Expected 'query' field in properties")
			}

			// Check required fields
			required, ok := params["required"].([]string)
			if !ok {
				t.Fatal("Parameters should have required field")
			}

			if len(required) != 1 || required[0] != "query" {
				t.Errorf("Expected required to be ['query'], got %v", required)
			}
		})
	}
}

func TestPerplexitySearchTool_ToTool(t *testing.T) {
	config := PerplexitySearchConfig{
		MaxResults: 10,
	}

	tool := NewPerplexitySearch(config)
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

func TestPerplexitySearch_ProviderExecuted(t *testing.T) {
	tool := NewPerplexitySearch(PerplexitySearchConfig{})
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
