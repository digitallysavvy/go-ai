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
	// The input schema is TS's z.object({query: z.string().min(1).max(200),
	// num_results: z.number().int().min(1).max(25).optional()}) as zod's
	// toJSONSchema renders it. Provider tool calls are validated against it.
	properties := map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"minLength":   1,
			"maxLength":   200,
			"description": "Web search query. Must be between 1 and 200 characters.",
		},
		"num_results": map[string]interface{}{
			"type":        "integer",
			"minimum":     1,
			"maximum":     25,
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

// browserbaseSearchOutputSchemaJSON is the TS output schema (packages/gateway/src/tool/browserbase-search.ts) as zod's
// toJSONSchema renders it (draft-07, io "input"). ValidateUIMessages checks
// tool outputs against it, so nullable fields are anyOf [T, null] and the
// required lists match zod's non-optional fields.
const browserbaseSearchOutputSchemaJSON = `{
	"anyOf": [
		{
			"type": "object",
			"properties": {
				"query": {
					"type": "string"
				},
				"requestId": {
					"type": "string"
				},
				"results": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"id": {
								"type": "string"
							},
							"title": {
								"type": "string"
							},
							"url": {
								"type": "string"
							},
							"author": {
								"type": "string"
							},
							"favicon": {
								"type": "string"
							},
							"image": {
								"type": "string"
							},
							"publishedDate": {
								"type": "string"
							}
						},
						"required": [
							"id",
							"title",
							"url"
						]
					}
				}
			},
			"required": [
				"query",
				"requestId",
				"results"
			]
		},
		{
			"type": "object",
			"properties": {
				"error": {
					"type": "string",
					"enum": [
						"api_error",
						"configuration_error",
						"execution_error",
						"invalid_input",
						"rate_limit",
						"timeout",
						"unknown"
					]
				},
				"statusCode": {
					"type": "number"
				},
				"message": {
					"type": "string"
				}
			},
			"required": [
				"error",
				"message"
			]
		}
	]
}`

func browserbaseSearchOutputSchema() map[string]interface{} {
	return mustDecodeToolSchema(browserbaseSearchOutputSchemaJSON)
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
