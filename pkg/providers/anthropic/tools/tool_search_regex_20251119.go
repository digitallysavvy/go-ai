package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolSearchRegex20251119Input represents the input parameters for the regex tool search
type ToolSearchRegex20251119Input struct {
	// Pattern is a regex pattern to search for tools (Python re.search() syntax)
	Pattern string `json:"pattern"`

	// Limit is the maximum number of tools to return (optional)
	Limit *int `json:"limit,omitempty"`
}

// ToolSearchRegex20251119Output represents the output of the tool search
type ToolSearchRegex20251119Output struct {
	// Type is always "tool_reference"
	Type string `json:"type"`

	// ToolName is the name of the discovered tool
	ToolName string `json:"toolName"`
}

// ToolSearchRegex20251119 creates a tool search tool that uses regex patterns to find tools.
//
// The tool search tool enables Claude to work with hundreds or thousands of tools
// by dynamically discovering and loading them on-demand. Instead of loading all
// tool definitions into the context window upfront, Claude searches your tool
// catalog and loads only the tools it needs.
//
// When Claude uses this tool, it constructs regex patterns using Python's
// re.search() syntax (NOT natural language queries).
//
// Important:
//   - This tool should never have `deferLoading: true` in providerOptions
//   - Other tools should use `providerOptions: { anthropic: { deferLoading: true } }` to mark them for deferred loading
//   - The tool returns tool_reference objects that are automatically expanded into full tool definitions by the API
//   - Supports deferred results since tool expansion happens in later turns
//   - Patterns are limited to 200 characters maximum
//
// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
//
// Example:
//
//	searchTool := anthropicTools.ToolSearchRegex20251119()
//
//	// In your tool configuration:
//	tools := map[string]types.Tool{
//	    "toolSearch": searchTool,
//	    "get_weather_data": weatherTool, // with deferLoading: true
//	    "get_user_data": userTool, // with deferLoading: true
//	    // ... hundreds more tools with deferLoading: true
//	}
//
// See: https://docs.anthropic.com/en/docs/agents-and-tools/tool-search-tool
func ToolSearchRegex20251119() types.Tool {
	parameters := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "A regex pattern to search for tools. Uses Python re.search() syntax. Maximum 200 characters.\n\nExamples:\n- \"weather\" - matches tool names/descriptions containing \"weather\"\n- \"get_.*_data\" - matches tools like get_user_data, get_weather_data\n- \"database.*query|query.*database\" - OR patterns for flexibility\n- \"(?i)slack\" - case-insensitive search",
				"maxLength":   200,
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of tools to return. Optional.",
			},
		},
		"required": []string{"pattern"},
	}

	tool := types.Tool{
		Name: "anthropic.tool_search_regex_20251119",
		Description: `Regex tool search for discovering tools using regex patterns.

This tool enables Claude to work with large tool catalogs (hundreds or thousands of tools)
by searching and loading tools on-demand rather than loading everything upfront.

How it works:
1. Claude constructs regex patterns using Python re.search() syntax
2. Patterns are matched against tool names and descriptions
3. Returns tool_reference objects that get automatically expanded by the API
4. Only discovered tools are loaded into the context window

Pattern examples:
- "weather" - simple substring match
- "get_.*_data" - matches get_user_data, get_weather_data, etc.
- "database.*query|query.*database" - OR patterns
- "(?i)slack" - case-insensitive matching
- "^create_" - tools starting with "create_"

Important notes:
- Use regex patterns (Python re.search() syntax), not natural language
- Maximum pattern length: 200 characters
- This tool should NOT have deferLoading enabled
- Other tools should have deferLoading: true to work with tool search
- The API handles tool expansion automatically

Best practices:
- Mark tools for deferred loading using providerOptions
- Use structured naming conventions (e.g., verb_object_action)
- Test patterns against your tool names
- Consider case-insensitive patterns with (?i) prefix
- Use OR patterns (|) for flexibility`,
		Parameters:       parameters,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("tool search regex must be executed by the provider (Anthropic). Set ProviderExecuted: true")
		},
		ProviderExecuted: true,
	}

	return tool
}
