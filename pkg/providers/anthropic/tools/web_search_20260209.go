package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// WebSearch20260209Config configures the web_search_20260209 provider tool.
type WebSearch20260209Config struct {
	// MaxUses is the maximum number of web searches Claude can perform during the conversation.
	MaxUses *int

	// AllowedDomains restricts searches to these domains only (optional).
	AllowedDomains []string

	// BlockedDomains prevents Claude from searching these domains (optional).
	BlockedDomains []string

	// UserLocation provides geographic context for more relevant results (optional).
	UserLocation *WebSearchUserLocation
}

// WebSearchUserLocation provides approximate geographic location for web search.
// Type must always be "approximate".
type WebSearchUserLocation struct {
	// Type must be "approximate"
	Type string

	// City name (optional)
	City string

	// Region or state (optional)
	Region string

	// Country code (optional, e.g. "US")
	Country string

	// Timezone is an IANA timezone ID (optional, e.g. "America/New_York")
	Timezone string
}

// WebSearchResult20260209 represents a single result from a web_search_20260209 tool call.
// When using multi-turn citations, the EncryptedContent field must be passed back in
// subsequent requests — it is required by Anthropic for citation functionality.
type WebSearchResult20260209 struct {
	// Type is always "web_search_result"
	Type string `json:"type"`

	// URL of the source page
	URL string `json:"url"`

	// Title of the source page (may be nil)
	Title *string `json:"title"`

	// PageAge indicates when the page was last updated (may be nil)
	PageAge *string `json:"pageAge"`

	// EncryptedContent must be passed back in multi-turn conversations for citations.
	// Do not modify or discard this value.
	EncryptedContent string `json:"encryptedContent"`
}

// webSearch20260209Opts stores the tool configuration and implements ToAnthropicAPIMap
// for the Anthropic tool converter.
type webSearch20260209Opts struct {
	Config WebSearch20260209Config
}

// ToAnthropicAPIMap returns the Anthropic API representation of this tool.
// Called by the Anthropic provider's tool converter.
func (o *webSearch20260209Opts) ToAnthropicAPIMap() map[string]interface{} {
	m := map[string]interface{}{
		"type": "web_search_20260209",
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
		loc := map[string]interface{}{
			"type": o.Config.UserLocation.Type,
		}
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

// ParseWebSearchResults parses a JSON-encoded array of web_search_result objects
// into typed WebSearchResult20260209 structs.
//
// Use this to decode the tool result returned when Claude calls web_search_20260209.
// Pass the parsed EncryptedContent back in subsequent requests to enable citations.
func ParseWebSearchResults(data []byte) ([]WebSearchResult20260209, error) {
	var results []WebSearchResult20260209
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("web_search_20260209 result parsing: %w", err)
	}
	return results, nil
}

// WebSearch20260209 creates an Anthropic web search provider tool (version 2026-02-09).
//
// This tool enables Claude to search the web for current information. The search is
// executed by Anthropic's servers automatically — no local execution required.
//
// For multi-turn conversations with citations, preserve the EncryptedContent field
// from each WebSearchResult20260209 and pass it back in subsequent requests.
//
// Tool ID: "anthropic.web_search_20260209"
//
// Example:
//
//	maxUses := 5
//	searchTool := tools.WebSearch20260209(tools.WebSearch20260209Config{
//	    MaxUses:        &maxUses,
//	    AllowedDomains: []string{"wikipedia.org", "docs.anthropic.com"},
//	    UserLocation: &tools.WebSearchUserLocation{
//	        Type:    "approximate",
//	        Country: "US",
//	    },
//	})
func WebSearch20260209(config WebSearch20260209Config) types.Tool {
	return types.Tool{
		Name:        "anthropic.web_search_20260209",
		Description: "Search the web for current information (Anthropic web search, version 2026-02-09). Executed by Anthropic's servers.",
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
			return nil, fmt.Errorf("web_search_20260209 is executed by the Anthropic provider, not locally")
		},
		ProviderExecuted:        true,
		SupportsDeferredResults: true,
		ProviderOptions:         &webSearch20260209Opts{Config: config},
	}
}
