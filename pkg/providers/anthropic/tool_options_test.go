package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	tools := []types.Tool{
		{
			Name:            "test_tool",
			Description:     "Test",
			Parameters:      map[string]interface{}{},
			ProviderOptions: nil,
		},
	}

	result := ToAnthropicFormatWithCache(tools)

	if len(result) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(result))
	}

	if _, hasCacheControl := result[0]["cache_control"]; hasCacheControl {
		t.Error("Expected no cache_control for nil ProviderOptions")
	}
}
