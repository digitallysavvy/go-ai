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
			name:   "default config",
			config: ParallelSearchConfig{},
		},
		{
			name: "one-shot mode",
			config: ParallelSearchConfig{
				Mode:       "one-shot",
				MaxResults: intPtr(10),
			},
		},
		{
			name: "agentic mode with source policy",
			config: ParallelSearchConfig{
				Mode:       "agentic",
				MaxResults: intPtr(5),
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
					MaxCharsPerResult: intPtr(500),
					MaxCharsTotal:     intPtr(5000),
				},
				FetchPolicy: &ParallelSearchFetchPolicy{
					MaxAgeSeconds: intPtr(3600),
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
			if typesTool.Name != "parallel_search" {
				t.Errorf("Expected tool name 'parallel_search', got %s", typesTool.Name)
			}
			if typesTool.Type != types.ToolTypeProviderDefined || typesTool.ProviderID != "gateway.parallel_search" {
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

func TestParallelSearchProviderArgsPreserveExplicitZeroNumericConfig(t *testing.T) {
	tool := NewParallelSearch(ParallelSearchConfig{
		MaxResults: intPtr(0),
		Excerpts: &ParallelSearchExcerpts{
			MaxCharsPerResult: intPtr(0),
			MaxCharsTotal:     intPtr(0),
		},
		FetchPolicy: &ParallelSearchFetchPolicy{
			MaxAgeSeconds: intPtr(0),
		},
	}).ToTool()
	if got, ok := tool.ProviderArgs["maxResults"]; !ok || got != 0 {
		t.Fatalf("provider args = %#v, want explicit maxResults:0", tool.ProviderArgs)
	}
	excerpts := tool.ProviderArgs["excerpts"].(map[string]interface{})
	if got, ok := excerpts["maxCharsPerResult"]; !ok || got != 0 {
		t.Fatalf("excerpts args = %#v, want explicit maxCharsPerResult:0", excerpts)
	}
	if got, ok := excerpts["maxCharsTotal"]; !ok || got != 0 {
		t.Fatalf("excerpts args = %#v, want explicit maxCharsTotal:0", excerpts)
	}
	fetchPolicy := tool.ProviderArgs["fetchPolicy"].(map[string]interface{})
	if got, ok := fetchPolicy["maxAgeSeconds"]; !ok || got != 0 {
		t.Fatalf("fetchPolicy args = %#v, want explicit maxAgeSeconds:0", fetchPolicy)
	}
}

func intPtr(v int) *int {
	return &v
}

func TestParallelSearchTool_ToTool(t *testing.T) {
	config := ParallelSearchConfig{
		Mode:       "one-shot",
		MaxResults: intPtr(10),
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

func TestParallelSearchSchemaMatchesTSOptionalFields(t *testing.T) {
	tool := NewParallelSearch(ParallelSearchConfig{}).ToTool()
	properties := tool.Parameters.(map[string]interface{})["properties"].(map[string]interface{})
	for _, name := range []string{"source_policy", "excerpts", "fetch_policy"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("missing TS optional field %q in schema: %#v", name, properties)
		}
	}
	if properties["max_results"].(map[string]interface{})["type"] != "number" {
		t.Fatalf("max_results schema = %#v", properties["max_results"])
	}
	excerpts := properties["excerpts"].(map[string]interface{})["properties"].(map[string]interface{})
	if excerpts["max_chars_per_result"].(map[string]interface{})["type"] != "number" {
		t.Fatalf("excerpts schema = %#v", excerpts)
	}
	fetchPolicy := properties["fetch_policy"].(map[string]interface{})["properties"].(map[string]interface{})
	if fetchPolicy["max_age_seconds"].(map[string]interface{})["type"] != "number" {
		t.Fatalf("fetch_policy schema = %#v", fetchPolicy)
	}
	output := tool.OutputSchema.(map[string]interface{})
	variants := output["oneOf"].([]interface{})
	if len(variants) != 2 {
		t.Fatalf("output schema should match TS success/error union: %#v", output)
	}
	resultProperties := variants[0].(map[string]interface{})["properties"].(map[string]interface{})["results"].(map[string]interface{})["items"].(map[string]interface{})["properties"].(map[string]interface{})
	if resultProperties["relevanceScore"].(map[string]interface{})["type"] != "number" {
		t.Fatalf("output result schema = %#v", resultProperties)
	}
}
