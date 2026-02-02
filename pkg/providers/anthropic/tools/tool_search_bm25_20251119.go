package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolSearchBm2520251119Input represents the input parameters for the BM25 tool search
type ToolSearchBm2520251119Input struct {
	// Query is a natural language query to search for tools
	Query string `json:"query"`

	// Limit is the maximum number of tools to return (optional)
	Limit *int `json:"limit,omitempty"`
}

// ToolSearchBm2520251119Output represents the output of the tool search
type ToolSearchBm2520251119Output struct {
	// Type is always "tool_reference"
	Type string `json:"type"`

	// ToolName is the name of the discovered tool
	ToolName string `json:"toolName"`
}

// ToolSearchBm2520251119 creates a tool search tool that uses BM25 (natural language) to find tools.
//
// The tool search tool enables Claude to work with hundreds or thousands of tools
// by dynamically discovering and loading them on-demand. Instead of loading all
// tool definitions into the context window upfront, Claude searches your tool
// catalog and loads only the tools it needs.
//
// When Claude uses this tool, it uses natural language queries (NOT regex patterns)
// to search for tools using BM25 text search.
//
// Important:
//   - This tool should never have `deferLoading: true` in providerOptions
//   - Other tools should use `providerOptions: { anthropic: { deferLoading: true } }` to mark them for deferred loading
//   - The tool returns tool_reference objects that are automatically expanded into full tool definitions by the API
//   - Supports deferred results since tool expansion happens in later turns
//
// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
//
// Example:
//
//	searchTool := anthropicTools.ToolSearchBm2520251119()
//
//	// In your tool configuration:
//	tools := map[string]types.Tool{
//	    "toolSearch": searchTool,
//	    "weatherTool": weatherTool, // with deferLoading: true
//	    "databaseTool": databaseTool, // with deferLoading: true
//	    // ... hundreds more tools with deferLoading: true
//	}
//
// See: https://docs.anthropic.com/en/docs/agents-and-tools/tool-search-tool
func ToolSearchBm2520251119() types.Tool {
	parameters := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "A natural language query to search for tools. Claude will use BM25 text search to find relevant tools.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of tools to return. Optional.",
			},
		},
		"required": []string{"query"},
	}

	tool := types.Tool{
		Name: "anthropic.tool_search_bm25_20251119",
		Description: `BM25 tool search for discovering tools using natural language queries.

This tool enables Claude to work with large tool catalogs (hundreds or thousands of tools)
by searching and loading tools on-demand rather than loading everything upfront.

How it works:
1. Claude uses natural language queries to search for relevant tools
2. The search uses BM25 algorithm for text-based relevance ranking
3. Returns tool_reference objects that get automatically expanded by the API
4. Only discovered tools are loaded into the context window

Query examples:
- "tools for weather data"
- "database query tools"
- "file operations"
- "API integrations for Slack"

Important notes:
- Use natural language queries, not regex patterns
- This tool should NOT have deferLoading enabled
- Other tools should have deferLoading: true to work with tool search
- The API handles tool expansion automatically

Best practices:
- Mark tools for deferred loading using providerOptions
- Keep tool names and descriptions searchable
- Use consistent naming conventions
- Include relevant keywords in tool descriptions`,
		Parameters:       parameters,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("tool search BM25 must be executed by the provider (Anthropic). Set ProviderExecuted: true")
		},
		ProviderExecuted: true,
	}

	return tool
}
