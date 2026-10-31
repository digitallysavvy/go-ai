package tools

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BrowserbaseFetchConfig configures a Browserbase Fetch provider-defined
// tool (TS BrowserbaseFetchConfig, packages/gateway/src/tool/browserbase-fetch.ts).
type BrowserbaseFetchConfig struct {
	// AllowRedirects controls whether to follow HTTP redirects (default: false).
	AllowRedirects *bool
	// AllowInsecureSSL controls whether to bypass TLS certificate verification
	// (default: false).
	AllowInsecureSSL *bool
	// Proxies controls whether to route the request through Browserbase
	// proxies (default: false).
	Proxies *bool
	// Format is the output format for the response content: "raw", "json",
	// or "markdown" (default: raw).
	Format string
	// Schema is a JSON Schema describing the desired response content. Only
	// used when Format is "json".
	Schema map[string]interface{}
}

// BrowserbaseFetchTool is the gateway.browserbase_fetch provider-defined
// tool.
type BrowserbaseFetchTool types.Tool

// NewBrowserbaseFetch creates a Browserbase Fetch tool with the given
// configuration. It mirrors the TypeScript SDK's
// browserbaseFetch/browserbaseFetchToolFactory
// (packages/gateway/src/tool/browserbase-fetch.ts).
func NewBrowserbaseFetch(config BrowserbaseFetchConfig) BrowserbaseFetchTool {
	// url is TS z.string().url(), which zod's toJSONSchema renders as
	// format "uri". Provider tool calls are validated against this schema.
	properties := map[string]interface{}{
		"url": map[string]interface{}{
			"type":        "string",
			"format":      "uri",
			"description": "URL of the page to fetch.",
		},
		"allow_redirects": map[string]interface{}{
			"type":        "boolean",
			"description": "Whether to follow HTTP redirects (default: false).",
		},
		"allow_insecure_ssl": map[string]interface{}{
			"type":        "boolean",
			"description": "Whether to bypass TLS certificate verification (default: false). Only use for trusted hosts.",
		},
		"proxies": map[string]interface{}{
			"type":        "boolean",
			"description": "Whether to route the request through Browserbase proxies (default: false).",
		},
		"format": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"raw", "json", "markdown"},
			"description": "Output format. raw returns the response body unchanged, markdown returns page content as Markdown, and json returns structured content using schema.",
		},
		"schema": map[string]interface{}{
			"type":                 "object",
			"propertyNames":        map[string]interface{}{"type": "string"},
			"additionalProperties": map[string]interface{}{},
			"description":          "JSON Schema for structured extraction. Only use with format set to json.",
		},
	}

	tool := types.Tool{
		Name:             "browserbase_fetch",
		Description:      "Fetch a web page using Browserbase and return its content as raw, markdown, or structured JSON.",
		Title:            "Browserbase Fetch",
		Parameters:       map[string]interface{}{"type": "object", "properties": properties, "required": []string{"url"}},
		OutputSchema:     browserbaseFetchOutputSchema(),
		ProviderExecuted: true,
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       "gateway.browserbase_fetch",
		ProviderArgs:     browserbaseFetchArgs(config),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "browserbase_fetch",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}
	return BrowserbaseFetchTool(tool)
}

// browserbaseFetchOutputSchemaJSON is the TS output schema (packages/gateway/src/tool/browserbase-fetch.ts) as zod's
// toJSONSchema renders it (draft-07, io "input"). ValidateUIMessages checks
// tool outputs against it, so nullable fields are anyOf [T, null] and the
// required lists match zod's non-optional fields.
const browserbaseFetchOutputSchemaJSON = `{
	"anyOf": [
		{
			"type": "object",
			"properties": {
				"id": {
					"type": "string"
				},
				"content": {
					"anyOf": [
						{
							"type": "string"
						},
						{
							"type": "object",
							"propertyNames": {
								"type": "string"
							},
							"additionalProperties": {}
						}
					]
				},
				"contentType": {
					"type": "string"
				},
				"encoding": {
					"type": "string"
				},
				"headers": {
					"type": "object",
					"propertyNames": {
						"type": "string"
					},
					"additionalProperties": {
						"type": "string"
					}
				},
				"statusCode": {
					"type": "number"
				}
			},
			"required": [
				"id",
				"content",
				"contentType",
				"encoding",
				"headers",
				"statusCode"
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

func browserbaseFetchOutputSchema() map[string]interface{} {
	return mustDecodeToolSchema(browserbaseFetchOutputSchemaJSON)
}

func browserbaseFetchArgs(config BrowserbaseFetchConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.AllowRedirects != nil {
		args["allowRedirects"] = *config.AllowRedirects
	}
	if config.AllowInsecureSSL != nil {
		args["allowInsecureSsl"] = *config.AllowInsecureSSL
	}
	if config.Proxies != nil {
		args["proxies"] = *config.Proxies
	}
	if config.Format != "" {
		args["format"] = config.Format
	}
	if config.Schema != nil {
		args["schema"] = config.Schema
	}
	return args
}

func (t BrowserbaseFetchTool) ToTool() types.Tool {
	return types.Tool(t)
}
