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
				MaxResults:       intPtr(10),
				MaxTokensPerPage: intPtr(2048),
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
				MaxResults:           intPtr(20),
				MaxTokensPerPage:     intPtr(1024),
				MaxTokens:            intPtr(10000),
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
			if typesTool.Name != "perplexity_search" {
				t.Errorf("Expected tool name 'perplexity_search', got %s", typesTool.Name)
			}
			if typesTool.Type != types.ToolTypeProviderDefined || typesTool.ProviderID != "gateway.perplexity_search" {
				t.Fatalf("provider tool identity = type:%q id:%q", typesTool.Type, typesTool.ProviderID)
			}

			// Check provider executed flag
			if !typesTool.ProviderExecuted {
				t.Error("Expected ProviderExecuted to be true")
			}

			// Check parameters exist
			if typesTool.Parameters == nil {
				t.Error("Expected Parameters to be set")
			}
			if typesTool.OutputSchema == nil {
				t.Fatal("Expected OutputSchema to be set for TS provider-executed tool parity")
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
		MaxResults: intPtr(10),
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

func TestPerplexitySearchProviderArgsPreserveExplicitZeroNumericConfig(t *testing.T) {
	tool := NewPerplexitySearch(PerplexitySearchConfig{
		MaxResults:       intPtr(0),
		MaxTokensPerPage: intPtr(0),
		MaxTokens:        intPtr(0),
	}).ToTool()
	for _, name := range []string{"maxResults", "maxTokensPerPage", "maxTokens"} {
		if got, ok := tool.ProviderArgs[name]; !ok || got != 0 {
			t.Fatalf("provider args = %#v, want explicit %s:0", tool.ProviderArgs, name)
		}
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

func TestPerplexitySearchSchemaMatchesTSNumericFields(t *testing.T) {
	tool := NewPerplexitySearch(PerplexitySearchConfig{}).ToTool()
	properties := tool.Parameters.(map[string]interface{})["properties"].(map[string]interface{})
	for _, name := range []string{"max_results", "max_tokens_per_page", "max_tokens"} {
		field := properties[name].(map[string]interface{})
		if field["type"] != "number" {
			t.Fatalf("%s schema = %#v", name, field)
		}
		if _, ok := field["minimum"]; ok {
			t.Fatalf("%s should not include non-TS minimum: %#v", name, field)
		}
		if _, ok := field["maximum"]; ok {
			t.Fatalf("%s should not include non-TS maximum: %#v", name, field)
		}
	}
	query := properties["query"].(map[string]interface{})
	for _, option := range query["oneOf"].([]map[string]interface{}) {
		if _, ok := option["maxItems"]; ok {
			t.Fatalf("query schema should not include non-TS maxItems: %#v", query)
		}
	}
	output := tool.OutputSchema.(map[string]interface{})
	variants := output["oneOf"].([]interface{})
	if len(variants) != 2 {
		t.Fatalf("output schema should match TS success/error union: %#v", output)
	}
	errorProperties := variants[1].(map[string]interface{})["properties"].(map[string]interface{})
	errorEnum := errorProperties["error"].(map[string]interface{})["enum"].([]string)
	if containsString(errorEnum, "configuration_error") {
		t.Fatalf("perplexity output error enum should match TS: %#v", errorEnum)
	}
}
