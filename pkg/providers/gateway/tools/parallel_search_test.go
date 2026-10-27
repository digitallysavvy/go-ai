package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
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
	if _, ok := resultProperties["excerpts"]; !ok {
		t.Fatalf("output result schema = %#v", resultProperties)
	}
}

// TestParallelSearchSourcePolicyDescriptionsMatchTS pins the source_policy
// descriptions (updated for the GA API in TS #23817).
func TestParallelSearchSourcePolicyDescriptionsMatchTS(t *testing.T) {
	tool := NewParallelSearch(ParallelSearchConfig{}).ToTool()
	properties := tool.Parameters.(map[string]interface{})["properties"].(map[string]interface{})
	sourcePolicy := properties["source_policy"].(map[string]interface{})["properties"].(map[string]interface{})

	const domainDescription = "Domains or domain/path prefixes to restrict results to, without scheme or port. Example: ['wikipedia.org', 'docs.python.org/3']."
	if got := sourcePolicy["include_domains"].(map[string]interface{})["description"]; got != domainDescription {
		t.Fatalf("include_domains description = %q, want %q", got, domainDescription)
	}
	const excludeDescription = "Domains or domain/path prefixes to exclude, without scheme or port. Example: ['reddit.com']."
	if got := sourcePolicy["exclude_domains"].(map[string]interface{})["description"]; got != excludeDescription {
		t.Fatalf("exclude_domains description = %q, want %q", got, excludeDescription)
	}
	const afterDateDescription = "Only include results published on or after this date. Use an ISO 8601 calendar date formatted YYYY-MM-DD (e.g. 2025-01-01); do not include a time."
	if got := sourcePolicy["after_date"].(map[string]interface{})["description"]; got != afterDateDescription {
		t.Fatalf("after_date description = %q, want %q", got, afterDateDescription)
	}
}

// Ported from TS #23817 (parallel-search.test.ts): parallelSearch follows
// Parallel's GA Search API.

func TestParallelSearchGAProviderArgs(t *testing.T) {
	tool := NewParallelSearch(ParallelSearchConfig{
		Mode:          ParallelSearchModeFast,
		Location:      "us",
		MaxCharsTotal: intPtr(8000),
		FetchPolicy: &ParallelSearchFetchPolicy{
			TimeoutSeconds:       intPtr(10),
			DisableCacheFallback: boolPtr(true),
		},
	}).ToTool()
	if tool.ProviderID != "gateway.parallel_search" || !tool.ProviderExecuted {
		t.Fatalf("tool identity = %q executed=%v", tool.ProviderID, tool.ProviderExecuted)
	}
	args := tool.ProviderArgs
	if args["mode"] != "fast" || args["location"] != "us" || args["maxCharsTotal"] != 8000 {
		t.Fatalf("args = %#v", args)
	}
	fp := args["fetchPolicy"].(map[string]interface{})
	if fp["timeoutSeconds"] != 10 || fp["disableCacheFallback"] != true {
		t.Fatalf("fetchPolicy = %#v", fp)
	}
}

func validateParallel(t *testing.T, s interface{}, valueJSON string) error {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(valueJSON), &v); err != nil {
		t.Fatal(err)
	}
	return schema.NewSimpleJSONSchema(s.(map[string]interface{})).Validator().Validate(v)
}

func TestParallelSearchInputSchemaModes(t *testing.T) {
	params := NewParallelSearch(ParallelSearchConfig{}).ToTool().Parameters
	for _, mode := range []string{"turbo", "fast", "basic", "advanced", "one-shot", "agentic"} {
		if err := validateParallel(t, params, `{"objective":"x","mode":"`+mode+`"}`); err != nil {
			t.Errorf("mode %s rejected: %v", mode, err)
		}
	}
	if err := validateParallel(t, params, `{"objective":"x","mode":"deep"}`); err == nil {
		t.Error("unknown mode should be rejected")
	}
	in := `{"objective":"x","search_queries":["q"],"location":"us","max_chars_total":6000,` +
		`"fetch_policy":{"max_age_seconds":600,"timeout_seconds":10,"disable_cache_fallback":true},` +
		`"source_policy":{"include_domains":["docs.python.org/3"]}}`
	if err := validateParallel(t, params, in); err != nil {
		t.Errorf("GA input rejected: %v", err)
	}
	props := params.(map[string]interface{})["properties"].(map[string]interface{})
	inc := props["source_policy"].(map[string]interface{})["properties"].(map[string]interface{})["include_domains"].(map[string]interface{})
	if !strings.Contains(inc["description"].(string), "domain/path") {
		t.Errorf("include_domains description = %v", inc["description"])
	}
}

func TestParallelSearchOutputSchema(t *testing.T) {
	out := NewParallelSearch(ParallelSearchConfig{}).ToTool().OutputSchema
	valid := map[string]string{
		"beta":          `{"search_id":"s1","results":[{"url":"https://e.com","title":"T","excerpts":["a"]}],"usage":[{"name":"sku_search","count":1}]}`,
		"ga null":       `{"search_id":"s2","results":[{"url":"https://e.com","title":null,"publish_date":null,"excerpts":["a","b"]},{"url":"https://e.com/b","title":"B","publish_date":"2026-01-15","excerpts":[]}],"warnings":null,"usage":[{"name":"sku_search","count":1}],"session_id":"x"}`,
		"ga warnings":   `{"search_id":"s3","results":[],"warnings":[{"type":"input_validation_warning","message":"m","detail":{"field":"location"}}],"session_id":"x"}`,
		"execution_err": `{"error":"execution_error","message":"Search failed"}`,
		"timeout":       `{"error":"timeout","message":"t","statusCode":504}`,
		"rate limit":    `{"error":"gateway_rate_limit","message":"Too many requests"}`,
	}
	for name, v := range valid {
		if err := validateParallel(t, out, v); err != nil {
			t.Errorf("%s rejected: %v", name, err)
		}
	}
	for name, v := range map[string]string{
		"no search_id": `{"results":[{"url":"https://e.com","excerpts":["a"]}]}`,
		"old shape":    `{"searchId":"s","results":[{"url":"https://e.com","title":"T","excerpt":"E"}]}`,
	} {
		if err := validateParallel(t, out, v); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
	b, _ := json.Marshal(out)
	for _, bad := range []string{"relevanceScore", "searchId", "publishDate", `"excerpt"`} {
		if strings.Contains(string(b), bad) {
			t.Errorf("output schema still declares %s", bad)
		}
	}
}
