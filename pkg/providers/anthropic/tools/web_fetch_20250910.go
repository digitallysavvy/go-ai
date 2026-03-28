package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// webFetch20250910Opts stores the tool configuration and implements ToAnthropicAPIMap
// for the Anthropic tool converter.
type webFetch20250910Opts struct {
	Config WebFetch20260209Config
}

// ToAnthropicAPIMap returns the Anthropic API representation of this tool.
func (o *webFetch20250910Opts) ToAnthropicAPIMap() map[string]interface{} {
	m := map[string]interface{}{
		"type": "web_fetch_20250910",
		"name": "web_fetch",
	}
	if o.Config.MaxUses != nil {
		m["max_uses"] = *o.Config.MaxUses
	}
	if len(o.Config.AllowedDomains) > 0 {
		m["allowed_domains"] = o.Config.AllowedDomains
	}
	if len(o.Config.BlockedDomains) > 0 {
		m["blocked_domains"] = o.Config.BlockedDomains
	}
	if o.Config.Citations != nil {
		m["citations"] = map[string]interface{}{
			"enabled": o.Config.Citations.Enabled,
		}
	}
	if o.Config.MaxContentTokens != nil {
		m["max_content_tokens"] = *o.Config.MaxContentTokens
	}
	return m
}

// WebFetch20250910 creates an Anthropic web fetch provider tool (version 2025-09-10).
//
// This tool enables Claude to fetch and read content from web URLs. The fetch is
// executed by Anthropic's servers automatically — no local execution required.
//
// Requires beta header: web-fetch-2025-09-10 (injected automatically).
//
// For the latest version, prefer WebFetch20260209.
//
// Tool ID: "anthropic.web_fetch_20250910"
func WebFetch20250910(config WebFetch20260209Config) types.Tool {
	return types.Tool{
		Name:        "anthropic.web_fetch_20250910",
		Description: "Fetch and read content from a URL (Anthropic web fetch, version 2025-09-10). Returns PDF or plain text content. Executed by Anthropic's servers.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to fetch.",
				},
			},
			"required": []string{"url"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("web_fetch_20250910 is executed by the Anthropic provider, not locally")
		},
		ProviderExecuted: true,
		ProviderOptions:  &webFetch20250910Opts{Config: config},
	}
}

// ParseWebFetch20250910Result parses a JSON-encoded web_fetch_result object into a typed
// WebFetchResult20260209 struct (same result schema as 20260209).
func ParseWebFetch20250910Result(data []byte) (*WebFetchResult20260209, error) {
	var result WebFetchResult20260209
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("web_fetch_20250910 result parsing: %w", err)
	}
	return &result, nil
}
