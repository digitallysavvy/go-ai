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
	properties := map[string]interface{}{
		"url": map[string]interface{}{
			"type":        "string",
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
			"type":        "object",
			"description": "JSON Schema for structured extraction. Only use with format set to json.",
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

func browserbaseFetchOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"oneOf": []interface{}{
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{"type": "string"},
					"content": map[string]interface{}{
						"oneOf": []interface{}{
							map[string]interface{}{"type": "string"},
							map[string]interface{}{"type": "object"},
						},
					},
					"contentType": map[string]interface{}{"type": "string"},
					"encoding":    map[string]interface{}{"type": "string"},
					"headers":     map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
					"statusCode":  map[string]interface{}{"type": "number"},
				},
				"required": []string{"id", "content", "contentType", "encoding", "headers", "statusCode"},
			},
			searchErrorSchema([]string{"api_error", "configuration_error", "execution_error", "invalid_input", "rate_limit", "timeout", "unknown"}),
		},
	}
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
