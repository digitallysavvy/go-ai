package anthropic

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// ToolReference creates a tool-reference content block for deferred tool loading.
//
// Tool references allow you to reference previously defined tools without
// sending the full tool definition again, reducing token usage and improving
// performance for tools that have already been defined in the conversation.
//
// This is an Anthropic-specific feature that enables efficient tool management
// in long conversations or when using tool search features.
//
// Example - Basic tool reference:
//
//	result := types.ContentResult("call_123", "tool_search",
//	    types.TextContentBlock{Text: "Found matching tools:"},
//	    anthropic.ToolReference("calculator"),
//	    anthropic.ToolReference("weather_api"),
//	)
//
// Example - Combined with tool search:
//
//	// Tool search returns tool references instead of full definitions
//	result := types.ContentResult("call_456", "search_tools",
//	    types.TextContentBlock{Text: "2 tools found for math operations:"},
//	    anthropic.ToolReference("add"),
//	    anthropic.ToolReference("multiply"),
//	)
//
// The tool-reference will be converted to Anthropic's API format:
//
//	{
//	    "type": "tool_reference",
//	    "tool_name": "calculator"
//	}
//
// Requirements:
//   - The referenced tool must have been defined earlier in the conversation
//   - Tool names must match exactly
//   - Only supported by Claude models via Anthropic API
func ToolReference(toolName string) types.CustomContentBlock {
	return types.CustomContentBlock{
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"type":     "tool-reference",
				"toolName": toolName,
			},
		},
	}
}

// IsToolReference checks if a custom content block is an Anthropic tool-reference
// and returns the tool name if it is.
//
// This helper function is useful for inspecting tool results to determine
// if they contain tool references.
//
// Example:
//
//	for _, block := range result.Output.Content {
//	    if customBlock, ok := block.(types.CustomContentBlock); ok {
//	        if toolName, isRef := anthropic.IsToolReference(customBlock); isRef {
//	            fmt.Printf("Found tool reference: %s\n", toolName)
//	        }
//	    }
//	}
//
// Returns:
//   - toolName: The name of the referenced tool (empty if not a tool-reference)
//   - isToolRef: true if this is a tool-reference, false otherwise
func IsToolReference(block types.CustomContentBlock) (toolName string, isToolRef bool) {
	// Check for anthropic provider options
	anthropicOpts, ok := block.ProviderOptions["anthropic"]
	if !ok {
		return "", false
	}

	// Convert to map
	optsMap, ok := anthropicOpts.(map[string]interface{})
	if !ok {
		return "", false
	}

	// Check if type is tool-reference
	if optsMap["type"] != "tool-reference" {
		return "", false
	}

	// Extract tool name
	if name, ok := optsMap["toolName"].(string); ok {
		return name, true
	}

	return "", false
}

// ExtractToolReferences extracts all tool references from a tool result
//
// This is a convenience function that scans a tool result's content blocks
// and returns all tool names that are referenced.
//
// Example:
//
//	toolNames := anthropic.ExtractToolReferences(result)
//	fmt.Printf("Referenced tools: %v\n", toolNames)
//
// Returns:
//   - A slice of tool names found in the content blocks
func ExtractToolReferences(result types.ToolResultContent) []string {
	if result.Output == nil || result.Output.Type != types.ToolResultOutputContent {
		return nil
	}

	var toolNames []string
	for _, block := range result.Output.Content {
		if customBlock, ok := block.(types.CustomContentBlock); ok {
			if toolName, isRef := IsToolReference(customBlock); isRef {
				toolNames = append(toolNames, toolName)
			}
		}
	}

	return toolNames
}
