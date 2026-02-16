package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestToolReference tests basic tool reference creation
func TestToolReference(t *testing.T) {
	ref := ToolReference("calculator")

	// Verify it's a CustomContentBlock
	if ref.ToolResultContentType() != "custom" {
		t.Errorf("ToolResultContentType() = %v, want 'custom'", ref.ToolResultContentType())
	}

	// Verify provider options structure
	anthropicOpts, ok := ref.ProviderOptions["anthropic"].(map[string]interface{})
	if !ok {
		t.Fatal("anthropic provider options should be a map")
	}

	if anthropicOpts["type"] != "tool-reference" {
		t.Errorf("type = %v, want 'tool-reference'", anthropicOpts["type"])
	}

	if anthropicOpts["toolName"] != "calculator" {
		t.Errorf("toolName = %v, want 'calculator'", anthropicOpts["toolName"])
	}
}

// TestIsToolReference tests tool reference detection
func TestIsToolReference(t *testing.T) {
	tests := []struct {
		name          string
		block         types.CustomContentBlock
		expectToolRef bool
		expectName    string
	}{
		{
			name:          "valid tool reference",
			block:         ToolReference("calculator"),
			expectToolRef: true,
			expectName:    "calculator",
		},
		{
			name: "not anthropic provider",
			block: types.CustomContentBlock{
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{
						"type": "something",
					},
				},
			},
			expectToolRef: false,
			expectName:    "",
		},
		{
			name: "wrong type",
			block: types.CustomContentBlock{
				ProviderOptions: map[string]interface{}{
					"anthropic": map[string]interface{}{
						"type": "other-type",
					},
				},
			},
			expectToolRef: false,
			expectName:    "",
		},
		{
			name: "missing toolName",
			block: types.CustomContentBlock{
				ProviderOptions: map[string]interface{}{
					"anthropic": map[string]interface{}{
						"type": "tool-reference",
					},
				},
			},
			expectToolRef: false,
			expectName:    "",
		},
		{
			name: "invalid provider options structure",
			block: types.CustomContentBlock{
				ProviderOptions: map[string]interface{}{
					"anthropic": "not-a-map",
				},
			},
			expectToolRef: false,
			expectName:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolName, isToolRef := IsToolReference(tt.block)

			if isToolRef != tt.expectToolRef {
				t.Errorf("IsToolReference() isToolRef = %v, want %v", isToolRef, tt.expectToolRef)
			}

			if toolName != tt.expectName {
				t.Errorf("IsToolReference() toolName = %v, want %v", toolName, tt.expectName)
			}
		})
	}
}

// TestExtractToolReferences tests extracting tool references from results
func TestExtractToolReferences(t *testing.T) {
	tests := []struct {
		name      string
		result    types.ToolResultContent
		wantNames []string
	}{
		{
			name: "single tool reference",
			result: types.ContentResult("call_123", "search",
				types.TextContentBlock{Text: "Found tool:"},
				ToolReference("calculator"),
			),
			wantNames: []string{"calculator"},
		},
		{
			name: "multiple tool references",
			result: types.ContentResult("call_456", "search",
				types.TextContentBlock{Text: "Found tools:"},
				ToolReference("add"),
				ToolReference("multiply"),
				ToolReference("divide"),
			),
			wantNames: []string{"add", "multiply", "divide"},
		},
		{
			name: "mixed content with tool references",
			result: types.ContentResult("call_789", "search",
				types.TextContentBlock{Text: "Results:"},
				ToolReference("weather"),
				types.TextContentBlock{Text: "More info:"},
				ToolReference("forecast"),
			),
			wantNames: []string{"weather", "forecast"},
		},
		{
			name: "no tool references",
			result: types.ContentResult("call_abc", "search",
				types.TextContentBlock{Text: "No tools found"},
			),
			wantNames: nil,
		},
		{
			name: "old style result",
			result: types.SimpleTextResult("call_def", "search", "simple text"),
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNames := ExtractToolReferences(tt.result)

			if len(gotNames) != len(tt.wantNames) {
				t.Errorf("ExtractToolReferences() got %d names, want %d", len(gotNames), len(tt.wantNames))
				return
			}

			for i, name := range gotNames {
				if name != tt.wantNames[i] {
					t.Errorf("ExtractToolReferences()[%d] = %v, want %v", i, name, tt.wantNames[i])
				}
			}
		})
	}
}

// TestToolReferenceInContentResult tests using tool references in real scenarios
func TestToolReferenceInContentResult(t *testing.T) {
	// Simulate a tool search result with tool references
	result := types.ContentResult("call_search_123", "tool_search",
		types.TextContentBlock{Text: "Found 3 math tools:"},
		ToolReference("add"),
		ToolReference("subtract"),
		ToolReference("multiply"),
	)

	// Verify structure
	if result.Output == nil {
		t.Fatal("Output should not be nil")
	}

	if result.Output.Type != types.ToolResultOutputContent {
		t.Errorf("Output.Type = %v, want %v", result.Output.Type, types.ToolResultOutputContent)
	}

	if len(result.Output.Content) != 4 {
		t.Errorf("Content blocks count = %d, want 4", len(result.Output.Content))
	}

	// Verify first block is text
	if _, ok := result.Output.Content[0].(types.TextContentBlock); !ok {
		t.Error("First block should be TextContentBlock")
	}

	// Verify next three blocks are tool references
	expectedTools := []string{"add", "subtract", "multiply"}
	for i, expectedTool := range expectedTools {
		customBlock, ok := result.Output.Content[i+1].(types.CustomContentBlock)
		if !ok {
			t.Errorf("Block %d should be CustomContentBlock", i+1)
			continue
		}

		toolName, isRef := IsToolReference(customBlock)
		if !isRef {
			t.Errorf("Block %d should be a tool reference", i+1)
		}
		if toolName != expectedTool {
			t.Errorf("Block %d tool name = %v, want %v", i+1, toolName, expectedTool)
		}
	}
}

// TestToolReferenceMultipleProviders tests that tool references are provider-specific
func TestToolReferenceMultipleProviders(t *testing.T) {
	// Create a custom block with multiple provider options
	customBlock := types.CustomContentBlock{
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"type":     "tool-reference",
				"toolName": "calc_anthropic",
			},
			"openai": map[string]interface{}{
				"type":     "function-reference",
				"funcName": "calc_openai",
			},
		},
	}

	// Should recognize Anthropic tool reference
	toolName, isRef := IsToolReference(customBlock)
	if !isRef {
		t.Error("Should recognize Anthropic tool reference")
	}
	if toolName != "calc_anthropic" {
		t.Errorf("Tool name = %v, want 'calc_anthropic'", toolName)
	}
}

// TestToolReferenceEmptyName tests edge case with empty tool name
func TestToolReferenceEmptyName(t *testing.T) {
	ref := ToolReference("")

	// Should create valid structure even with empty name
	if ref.ToolResultContentType() != "custom" {
		t.Error("Should create valid custom block even with empty name")
	}

	toolName, isRef := IsToolReference(ref)
	if !isRef {
		t.Error("Should be recognized as tool reference")
	}
	if toolName != "" {
		t.Errorf("Tool name = %v, want empty string", toolName)
	}
}

// TestToolReferenceNaming tests various tool name formats
func TestToolReferenceNaming(t *testing.T) {
	testNames := []string{
		"simple",
		"with_underscore",
		"with-dash",
		"with.dot",
		"CamelCase",
		"with123numbers",
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			ref := ToolReference(name)
			extractedName, isRef := IsToolReference(ref)

			if !isRef {
				t.Error("Should be recognized as tool reference")
			}
			if extractedName != name {
				t.Errorf("Tool name = %v, want %v", extractedName, name)
			}
		})
	}
}
