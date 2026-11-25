package tools

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DeepSeekFetchConfig configures a DeepSeek Fetch provider-executed tool (TS
// DeepSeekFetchConfig, packages/gateway/src/tool/deepseek-fetch.ts, TS
// 2099d455e4).
type DeepSeekFetchConfig struct {
	// URL pins the target URL instead of allowing the model to choose it.
	URL string
}

// DeepSeekFetchTool is the gateway.deepseek_fetch provider-executed tool.
type DeepSeekFetchTool types.Tool

// NewDeepSeekFetch creates a DeepSeek Fetch tool, which fetches a page as
// Markdown and is executed by AI Gateway for any model (TS deepseekFetch /
// deepseekFetchToolFactory).
func NewDeepSeekFetch(config DeepSeekFetchConfig) DeepSeekFetchTool {
	args := map[string]interface{}{}
	if config.URL != "" {
		args["url"] = config.URL
	}
	tool := types.Tool{
		Name:             "deepseek_fetch",
		Description:      "Fetch a page as Markdown through DeepSeek's standalone Fetch API.",
		Parameters:       mustDecodeToolSchema(deepseekFetchInputSchemaJSON),
		OutputSchema:     mustDecodeToolSchema(deepseekFetchOutputSchemaJSON),
		ProviderExecuted: true,
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       "gateway.deepseek_fetch",
		ProviderArgs:     args,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "deepseek_fetch",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}
	return DeepSeekFetchTool(tool)
}

// ToTool returns the tool as a types.Tool.
func (t DeepSeekFetchTool) ToTool() types.Tool {
	return types.Tool(t)
}

const deepseekFetchInputSchemaJSON = `{
	"type": "object",
	"properties": {
		"url": {
			"type": "string",
			"description": "URL to fetch, at most 8192 characters; HTTP(S) only, without a username or password."
		}
	},
	"required": [
		"url"
	]
}`

var deepseekFetchOutputSchemaJSON = `{
	"anyOf": [
		{
			"type": "object",
			"properties": {
				"title": ` + deepseekNullable("string") + `,
				"content": {
					"type": "string"
				}
			},
			"required": [
				"content"
			],
			"additionalProperties": {}
		},
		` + gatewayToolErrorSchemaJSON + `
	]
}`
