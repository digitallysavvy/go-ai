package tools

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BrowserbaseSearchConfig configures a Browserbase Search provider-defined
// tool (TS BrowserbaseSearchConfig, packages/gateway/src/tool/browserbase-search.ts).
type BrowserbaseSearchConfig struct {
	// NumResults is the default maximum number of results to return (1-25,
	// default: 10).
	NumResults *int
}

// BrowserbaseSearchTool is the gateway.browserbase_search provider-defined
// tool.
type BrowserbaseSearchTool types.Tool

// NewBrowserbaseSearch creates a Browserbase Search tool with the given
// configuration. It mirrors the TypeScript SDK's
// browserbaseSearch/browserbaseSearchToolFactory
// (packages/gateway/src/tool/browserbase-search.ts).
func NewBrowserbaseSearch(config BrowserbaseSearchConfig) BrowserbaseSearchTool {
	properties := map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": "Web search query. Must be between 1 and 200 characters.",
		},
		"num_results": map[string]interface{}{
			"type":        "number",
			"description": "Maximum number of results to return (1-25, default: 10).",
		},
	}

	tool := types.Tool{
		Name:             "browserbase_search",
		Description:      "Search the web using Browserbase for current information.",
		Title:            "Browserbase Search",
		Parameters:       map[string]interface{}{"type": "object", "properties": properties, "required": []string{"query"}},
		OutputSchema:     browserbaseSearchOutputSchema(),
		ProviderExecuted: true,
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       "gateway.browserbase_search",
		ProviderArgs:     browserbaseSearchArgs(config),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "browserbase_search",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}
	return BrowserbaseSearchTool(tool)
}

func browserbaseSearchOutputSchema() map[string]interface{} {
	resultProperties := map[string]interface{}{
		"id":            map[string]interface{}{"type": "string"},
		"title":         map[string]interface{}{"type": "string"},
		"url":           map[string]interface{}{"type": "string"},
		"author":        map[string]interface{}{"type": "string"},
		"favicon":       map[string]interface{}{"type": "string"},
		"image":         map[string]interface{}{"type": "string"},
		"publishedDate": map[string]interface{}{"type": "string"},
	}
	return map[string]interface{}{
		"oneOf": []interface{}{
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":     map[string]interface{}{"type": "string"},
					"requestId": map[string]interface{}{"type": "string"},
					"results":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object", "properties": resultProperties}},
				},
				"required": []string{"query", "requestId", "results"},
			},
			searchErrorSchema([]string{"api_error", "configuration_error", "execution_error", "invalid_input", "rate_limit", "timeout", "unknown"}),
		},
	}
}

func browserbaseSearchArgs(config BrowserbaseSearchConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.NumResults != nil {
		args["numResults"] = *config.NumResults
	}
	return args
}

func (t BrowserbaseSearchTool) ToTool() types.Tool {
	return types.Tool(t)
}
