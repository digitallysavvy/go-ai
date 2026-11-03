package tools

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DeepSeekSearchConfig configures a DeepSeek Search provider-executed tool
// (TS DeepSeekSearchConfig, packages/gateway/src/tool/deepseek-search.ts, TS
// 2099d455e4).
type DeepSeekSearchConfig struct {
	// Query pins the query instead of allowing the model to choose it.
	Query string
	// Count is the maximum number of results, 1-50 (default: 10).
	Count *int
	// NeedContent includes available page content (default: false).
	NeedContent *bool
	// OriQuestion is the original question, 1-128 Unicode characters.
	OriQuestion *string
	// Localization is a developer-configured IP address for result
	// localization.
	Localization string
}

// DeepSeekSearchTool is the gateway.deepseek_search provider-executed tool.
type DeepSeekSearchTool types.Tool

// NewDeepSeekSearch creates a DeepSeek Search tool, executed by AI Gateway for
// any model (TS deepseekSearch / deepseekSearchToolFactory).
func NewDeepSeekSearch(config DeepSeekSearchConfig) DeepSeekSearchTool {
	tool := types.Tool{
		Name:             "deepseek_search",
		Description:      "Search the web through DeepSeek's standalone Search API.",
		Parameters:       mustDecodeToolSchema(deepseekSearchInputSchemaJSON),
		OutputSchema:     mustDecodeToolSchema(deepseekSearchOutputSchemaJSON),
		ProviderExecuted: true,
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       "gateway.deepseek_search",
		ProviderArgs:     deepseekSearchArgs(config),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "deepseek_search",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}
	return DeepSeekSearchTool(tool)
}

func deepseekSearchArgs(config DeepSeekSearchConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.Query != "" {
		args["query"] = config.Query
	}
	if config.Count != nil {
		args["count"] = *config.Count
	}
	if config.NeedContent != nil {
		args["needContent"] = *config.NeedContent
	}
	if config.OriQuestion != nil {
		args["oriQuestion"] = *config.OriQuestion
	}
	if config.Localization != "" {
		args["localization"] = config.Localization
	}
	return args
}

// ToTool returns the tool as a types.Tool.
func (t DeepSeekSearchTool) ToTool() types.Tool {
	return types.Tool(t)
}

// deepseekSearchInputSchemaJSON is the TS input schema as zod's toJSONSchema
// renders it (int(): the safe-integer bounds).
const deepseekSearchInputSchemaJSON = `{
	"type": "object",
	"properties": {
		"query": {
			"type": "string",
			"description": "Web search query, 1 to 128 Unicode characters. Supports site:, -site:, lang:, after:, before:, and quoted phrases."
		},
		"count": {
			"type": "integer",
			"minimum": -9007199254740991,
			"maximum": 9007199254740991,
			"description": "Number of results, 1 to 50 (default: 10)."
		},
		"need_content": {
			"type": "boolean",
			"description": "Include available page content (default: false)."
		},
		"ori_question": {
			"anyOf": [
				{
					"type": "string"
				},
				{
					"type": "null"
				}
			],
			"description": "Original user question, 1 to 128 Unicode characters."
		}
	},
	"required": [
		"query"
	]
}`

func deepseekNullable(t string) string {
	return `{"anyOf": [{"type": "` + t + `"}, {"type": "null"}]}`
}

var deepseekSearchOutputSchemaJSON = `{
	"anyOf": [
		{
			"type": "object",
			"properties": {
				"results": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"authority": ` + deepseekNullable("number") + `,
							"content": ` + deepseekNullable("string") + `,
							"date": ` + deepseekNullable("string") + `,
							"rerank_score": ` + deepseekNullable("number") + `,
							"site_icon": ` + deepseekNullable("string") + `,
							"site_name": ` + deepseekNullable("string") + `,
							"snippet": ` + deepseekNullable("string") + `,
							"summary": ` + deepseekNullable("string") + `,
							"title": ` + deepseekNullable("string") + `,
							"url": ` + deepseekNullable("string") + `
						},
						"additionalProperties": {}
					}
				}
			},
			"required": [
				"results"
			],
			"additionalProperties": {}
		},
		` + gatewayToolErrorSchemaJSON + `
	]
}`
