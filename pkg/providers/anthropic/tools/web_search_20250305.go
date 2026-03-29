package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// webSearch20250305Opts stores the tool configuration and implements ToAnthropicAPIMap
// for the Anthropic tool converter.
type webSearch20250305Opts struct {
	Config WebSearch20260209Config
}

// ToAnthropicAPIMap returns the Anthropic API representation of this tool.
func (o *webSearch20250305Opts) ToAnthropicAPIMap() map[string]interface{} {
	m := map[string]interface{}{
		"type": "web_search_20250305",
		"name": "web_search",
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
	if o.Config.UserLocation != nil {
		loc := map[string]interface{}{"type": o.Config.UserLocation.Type}
		if o.Config.UserLocation.City != "" {
			loc["city"] = o.Config.UserLocation.City
		}
		if o.Config.UserLocation.Region != "" {
			loc["region"] = o.Config.UserLocation.Region
		}
		if o.Config.UserLocation.Country != "" {
			loc["country"] = o.Config.UserLocation.Country
		}
		if o.Config.UserLocation.Timezone != "" {
			loc["timezone"] = o.Config.UserLocation.Timezone
		}
		m["user_location"] = loc
	}
	return m
}

// WebSearch20250305 creates an Anthropic web search provider tool (version 2025-03-05).
//
// This tool enables Claude to search the web for current information. The search is
// executed by Anthropic's servers automatically — no local execution required.
// This version does not require a beta header.
//
// For multi-turn conversations with citations, preserve the EncryptedContent field
// from each WebSearchResult20260209 and pass it back in subsequent requests.
//
// For the latest version, prefer WebSearch20260209.
//
// Tool ID: "anthropic.web_search_20250305"
func WebSearch20250305(config WebSearch20260209Config) types.Tool {
	return types.Tool{
		Name:        "anthropic.web_search_20250305",
		Description: "Search the web for current information (Anthropic web search, version 2025-03-05). Executed by Anthropic's servers.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query to execute.",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("web_search_20250305 is executed by the Anthropic provider, not locally")
		},
		ProviderExecuted:        true,
		SupportsDeferredResults: true,
		ProviderOptions:         &webSearch20250305Opts{Config: config},
	}
}

// ParseWebSearch20250305Results parses a JSON-encoded array of web_search_result objects
// into typed WebSearchResult20260209 structs (same result schema as 20260209).
func ParseWebSearch20250305Results(data []byte) ([]WebSearchResult20260209, error) {
	var results []WebSearchResult20260209
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("web_search_20250305 result parsing: %w", err)
	}
	return results, nil
}
