package anthropic

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func TestWithCacheControl(t *testing.T) {
	tests := []struct {
		name        string
		ttl         string
		wantType    string
		wantTTL     string
		description string
	}{
		{
			name:        "default_cache_5m",
			ttl:         "",
			wantType:    "ephemeral",
			wantTTL:     "",
			description: "Default 5-minute cache when TTL is empty",
		},
		{
			name:        "explicit_5m_cache",
			ttl:         "5m",
			wantType:    "ephemeral",
			wantTTL:     "5m",
			description: "Explicit 5-minute cache TTL",
		},
		{
			name:        "1h_cache",
			ttl:         "1h",
			wantType:    "ephemeral",
			wantTTL:     "1h",
			description: "1-hour cache TTL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := WithCacheControl(tt.ttl)

			if opts == nil {
				t.Fatal("WithCacheControl returned nil")
			}

			if opts.CacheControl == nil {
				t.Fatal("CacheControl is nil")
			}

			if opts.CacheControl.Type != tt.wantType {
				t.Errorf("CacheControl.Type = %v, want %v", opts.CacheControl.Type, tt.wantType)
			}

			if opts.CacheControl.TTL != tt.wantTTL {
				t.Errorf("CacheControl.TTL = %v, want %v", opts.CacheControl.TTL, tt.wantTTL)
			}
		})
	}
}

func TestWithToolCache(t *testing.T) {
	tests := []struct {
		name    string
		ttl     string
		wantTTL string
	}{
		{
			name:    "default_cache",
			ttl:     "",
			wantTTL: "",
		},
		{
			name:    "5m_cache",
			ttl:     "5m",
			wantTTL: "5m",
		},
		{
			name:    "1h_cache",
			ttl:     "1h",
			wantTTL: "1h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := WithToolCache(tt.ttl)

			if opts == nil {
				t.Fatal("WithToolCache returned nil")
			}

			if opts.CacheControl == nil {
				t.Fatal("CacheControl is nil")
			}

			if opts.CacheControl.Type != "ephemeral" {
				t.Errorf("CacheControl.Type = %v, want ephemeral", opts.CacheControl.Type)
			}

			if opts.CacheControl.TTL != tt.wantTTL {
				t.Errorf("CacheControl.TTL = %v, want %v", opts.CacheControl.TTL, tt.wantTTL)
			}
		})
	}
}

func TestToAnthropicFormatWithCache(t *testing.T) {
	tests := []struct {
		name               string
		tools              []types.Tool
		expectCacheControl bool
		description        string
	}{
		{
			name: "tool_without_cache",
			tools: []types.Tool{
				{
					Name:        "get_weather",
					Description: "Get weather information",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{
								"type":        "string",
								"description": "City name",
							},
						},
						"required": []string{"location"},
					},
				},
			},
			expectCacheControl: false,
			description:        "Tool without ProviderOptions should not have cache_control",
		},
		{
			name: "tool_with_cache_5m",
			tools: []types.Tool{
				{
					Name:        "get_weather",
					Description: "Get weather information",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{
								"type":        "string",
								"description": "City name",
							},
						},
						"required": []string{"location"},
					},
					ProviderOptions: &ToolOptions{
						CacheControl: &CacheControl{
							Type: "ephemeral",
							TTL:  "5m",
						},
					},
				},
			},
			expectCacheControl: true,
			description:        "Tool with ProviderOptions should have cache_control",
		},
		{
			name: "tool_with_cache_1h",
			tools: []types.Tool{
				{
					Name:        "calculate",
					Description: "Perform calculations",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"expression": map[string]interface{}{
								"type":        "string",
								"description": "Math expression",
							},
						},
						"required": []string{"expression"},
					},
					ProviderOptions: WithCacheControl("1h"),
				},
			},
			expectCacheControl: true,
			description:        "Tool with 1h cache TTL",
		},
		{
			name: "mixed_tools",
			tools: []types.Tool{
				{
					Name:        "tool_cached",
					Description: "Cached tool",
					Parameters: map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					},
					ProviderOptions: WithToolCache("5m"),
				},
				{
					Name:        "tool_not_cached",
					Description: "Not cached tool",
					Parameters: map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					},
				},
			},
			expectCacheControl: false, // Not all tools have cache
			description:        "Mix of cached and non-cached tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToAnthropicFormatWithCache(tt.tools)

			if len(result) != len(tt.tools) {
				t.Fatalf("Expected %d tools, got %d", len(tt.tools), len(result))
			}

			for i, toolDef := range result {
				// Check basic fields
				if toolDef["name"] != tt.tools[i].Name {
					t.Errorf("Tool %d: name = %v, want %v", i, toolDef["name"], tt.tools[i].Name)
				}

				if toolDef["description"] != tt.tools[i].Description {
					t.Errorf("Tool %d: description = %v, want %v", i, toolDef["description"], tt.tools[i].Description)
				}

				if toolDef["input_schema"] == nil {
					t.Errorf("Tool %d: input_schema is nil", i)
				}

				// Check cache_control
				cacheControl, hasCacheControl := toolDef["cache_control"]

				if tt.tools[i].ProviderOptions != nil {
					if !hasCacheControl {
						t.Errorf("Tool %d: expected cache_control but not found", i)
						continue
					}

					// Verify cache_control structure
					cacheMap, ok := cacheControl.(*CacheControl)
					if !ok {
						t.Errorf("Tool %d: cache_control has wrong type: %T", i, cacheControl)
						continue
					}

					if cacheMap.Type != "ephemeral" {
						t.Errorf("Tool %d: cache_control.type = %v, want ephemeral", i, cacheMap.Type)
					}

					// Check TTL if set
					if opts, ok := tt.tools[i].ProviderOptions.(*ToolOptions); ok {
						if opts.CacheControl != nil && opts.CacheControl.TTL != "" {
							if cacheMap.TTL != opts.CacheControl.TTL {
								t.Errorf("Tool %d: cache_control.ttl = %v, want %v", i, cacheMap.TTL, opts.CacheControl.TTL)
							}
						}
					}
				} else {
					if hasCacheControl {
						t.Errorf("Tool %d: unexpected cache_control found", i)
					}
				}
			}
		})
	}
}

func TestToolOptionsNilSafety(t *testing.T) {
	// Test that nil ProviderOptions doesn't cause issues
	toolList := []types.Tool{
		{
			Name:            "test_tool",
			Description:     "Test",
			Parameters:      map[string]interface{}{},
			ProviderOptions: nil,
		},
	}

	result := ToAnthropicFormatWithCache(toolList)

	if len(result) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(result))
	}

	if _, hasCacheControl := result[0]["cache_control"]; hasCacheControl {
		t.Error("Expected no cache_control for nil ProviderOptions")
	}
}

// ---------------------------------------------------------------------------
// Beta header injection — web tools 20260209
// ---------------------------------------------------------------------------

// TestAnthropicWebTools20260209BetaHeaderInjected verifies that web_search_20260209
// and web_fetch_20260209 inject the code-execution-web-tools-2026-02-09 beta header,
// matching the TypeScript SDK behaviour in anthropic-prepare-tools.ts.
func TestAnthropicWebTools20260209BetaHeaderInjected(t *testing.T) {
	m := &LanguageModel{}
	tests := []struct {
		name string
		tool types.Tool
	}{
		{"web_search_20260209", tools.WebSearch20260209(tools.WebSearch20260209Config{})},
		{"web_fetch_20260209", tools.WebFetch20260209(tools.WebFetch20260209Config{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &provider.GenerateOptions{Tools: []types.Tool{tt.tool}}
			header := m.combineBetaHeaders(opts, false)
			if !headerContains(header, BetaHeaderWebTools20260209) {
				t.Errorf("%s must inject beta header %q, got %q", tt.name, BetaHeaderWebTools20260209, header)
			}
		})
	}
}

func TestWebToolsBetaHeaderAbsentForOtherTools(t *testing.T) {
	m := &LanguageModel{}
	opts := &provider.GenerateOptions{
		Tools: []types.Tool{
			{Name: "my_fn", Parameters: map[string]interface{}{"type": "object"}},
		},
	}
	header := m.combineBetaHeaders(opts, false)
	if headerContains(header, BetaHeaderWebTools20260209) {
		t.Errorf("web-tools beta should not appear for non-web tools, got %q", header)
	}
}

// ---------------------------------------------------------------------------
// DeferLoading serialization
// ---------------------------------------------------------------------------

func TestDeferLoadingTrueSerializedInTool(t *testing.T) {
	deferLoad := true
	tool := types.Tool{
		Name:            "my_function",
		Parameters:      map[string]interface{}{"type": "object"},
		ProviderOptions: &ToolOptions{DeferLoading: &deferLoad},
	}
	converted := ToAnthropicFormatWithCache([]types.Tool{tool})
	if converted[0]["defer_loading"] != true {
		t.Errorf("defer_loading = %v, want true", converted[0]["defer_loading"])
	}
}

func TestDeferLoadingFalseSerializedInTool(t *testing.T) {
	deferLoad := false
	tool := types.Tool{
		Name:            "my_function",
		Parameters:      map[string]interface{}{"type": "object"},
		ProviderOptions: &ToolOptions{DeferLoading: &deferLoad},
	}
	converted := ToAnthropicFormatWithCache([]types.Tool{tool})
	if converted[0]["defer_loading"] != false {
		t.Errorf("defer_loading = %v, want false", converted[0]["defer_loading"])
	}
}

func TestDeferLoadingAbsentByDefault(t *testing.T) {
	tool := types.Tool{Name: "my_function", Parameters: map[string]interface{}{"type": "object"}}
	converted := ToAnthropicFormatWithCache([]types.Tool{tool})
	if _, ok := converted[0]["defer_loading"]; ok {
		t.Error("defer_loading should not be present when DeferLoading is nil")
	}
}

// ---------------------------------------------------------------------------
// AllowedCallers serialization + beta header
// ---------------------------------------------------------------------------

func TestAllowedCallersSerializedInTool(t *testing.T) {
	tool := types.Tool{
		Name:       "my_function",
		Parameters: map[string]interface{}{"type": "object"},
		ProviderOptions: &ToolOptions{
			AllowedCallers: []string{"direct", "code_execution_20260120"},
		},
	}
	converted := ToAnthropicFormatWithCache([]types.Tool{tool})
	callers, ok := converted[0]["allowed_callers"].([]string)
	if !ok {
		t.Fatalf("allowed_callers type = %T, want []string", converted[0]["allowed_callers"])
	}
	if len(callers) != 2 || callers[0] != "direct" || callers[1] != "code_execution_20260120" {
		t.Errorf("allowed_callers = %v, want [direct code_execution_20260120]", callers)
	}
}

func TestAllowedCallersAbsentByDefault(t *testing.T) {
	tool := types.Tool{Name: "my_function", Parameters: map[string]interface{}{"type": "object"}}
	converted := ToAnthropicFormatWithCache([]types.Tool{tool})
	if _, ok := converted[0]["allowed_callers"]; ok {
		t.Error("allowed_callers should not be present when AllowedCallers is empty")
	}
}

func TestAllowedCallersBetaHeaderInjected(t *testing.T) {
	m := &LanguageModel{}
	opts := &provider.GenerateOptions{
		Tools: []types.Tool{{
			Name:            "my_function",
			Parameters:      map[string]interface{}{"type": "object"},
			ProviderOptions: &ToolOptions{AllowedCallers: []string{"direct"}},
		}},
	}
	header := m.combineBetaHeaders(opts, false)
	if !headerContains(header, BetaHeaderAdvancedToolUse) {
		t.Errorf("header %q does not contain %q", header, BetaHeaderAdvancedToolUse)
	}
}

func TestAllowedCallersBetaAbsentWhenEmpty(t *testing.T) {
	m := &LanguageModel{}
	opts := &provider.GenerateOptions{
		Tools: []types.Tool{{Name: "my_function", Parameters: map[string]interface{}{"type": "object"}}},
	}
	header := m.combineBetaHeaders(opts, false)
	if headerContains(header, BetaHeaderAdvancedToolUse) {
		t.Errorf("advanced-tool-use beta should not appear without AllowedCallers/InputExamples, got %q", header)
	}
}

// ---------------------------------------------------------------------------
// InputExamples serialization + beta header
// ---------------------------------------------------------------------------

func TestInputExamplesSerializedInTool(t *testing.T) {
	tool := types.Tool{
		Name:       "my_function",
		Parameters: map[string]interface{}{"type": "object"},
		InputExamples: []types.ToolInputExample{
			{Input: map[string]interface{}{"query": "hello world"}},
			{Input: map[string]interface{}{"query": "foo bar"}},
		},
	}
	converted := ToAnthropicFormatWithCache([]types.Tool{tool})
	examples, ok := converted[0]["input_examples"].([]interface{})
	if !ok {
		t.Fatalf("input_examples type = %T, want []interface{}", converted[0]["input_examples"])
	}
	if len(examples) != 2 {
		t.Fatalf("input_examples len = %d, want 2", len(examples))
	}
	first := examples[0].(map[string]interface{})
	if first["query"] != "hello world" {
		t.Errorf("input_examples[0][query] = %v, want hello world", first["query"])
	}
}

func TestInputExamplesAbsentByDefault(t *testing.T) {
	tool := types.Tool{Name: "my_function", Parameters: map[string]interface{}{"type": "object"}}
	converted := ToAnthropicFormatWithCache([]types.Tool{tool})
	if _, ok := converted[0]["input_examples"]; ok {
		t.Error("input_examples should not be present when InputExamples is empty")
	}
}

func TestInputExamplesBetaHeaderInjected(t *testing.T) {
	m := &LanguageModel{}
	opts := &provider.GenerateOptions{
		Tools: []types.Tool{{
			Name:          "my_function",
			Parameters:    map[string]interface{}{"type": "object"},
			InputExamples: []types.ToolInputExample{{Input: map[string]interface{}{"q": "test"}}},
		}},
	}
	header := m.combineBetaHeaders(opts, false)
	if !headerContains(header, BetaHeaderAdvancedToolUse) {
		t.Errorf("header %q does not contain %q", header, BetaHeaderAdvancedToolUse)
	}
}

func TestInputExamplesNotOnProviderTools(t *testing.T) {
	// Provider tools self-serialize; InputExamples must not bleed into their map.
	tool := tools.WebSearch20260209(tools.WebSearch20260209Config{})
	tool.InputExamples = []types.ToolInputExample{{Input: map[string]interface{}{"query": "test"}}}
	converted := ToAnthropicFormatWithCache([]types.Tool{tool})
	if _, ok := converted[0]["input_examples"]; ok {
		t.Error("input_examples must not appear on provider tools (they self-serialize)")
	}
}

// headerContains checks if target appears in a comma-separated header string.
func headerContains(headers, target string) bool {
	for _, h := range strings.Split(headers, ",") {
		if strings.TrimSpace(h) == target {
			return true
		}
	}
	return false
}
