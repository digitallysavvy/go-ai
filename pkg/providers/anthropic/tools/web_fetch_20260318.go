package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// WebFetch20260318Config configures the web_fetch_20260318 provider tool.
// It is a superset of WebFetch20260209Config, adding UseCache and ResponseInclusion.
type WebFetch20260318Config struct {
	// MaxUses limits the number of web fetches Claude can perform during the conversation.
	MaxUses *int

	// AllowedDomains restricts fetches to these domains only (optional).
	AllowedDomains []string

	// BlockedDomains prevents Claude from fetching these domains (optional).
	BlockedDomains []string

	// Citations enables citation of specific passages from fetched documents (optional).
	// Unlike web search where citations are always enabled, citations are optional for web fetch.
	Citations *WebFetchCitations

	// MaxContentTokens limits the amount of content included in the context (optional).
	MaxContentTokens *int

	// UseCache controls whether cached content may be returned. Set to false to
	// force fresh content. Defaults to true (Anthropic API default).
	UseCache *bool

	// ResponseInclusion controls whether web fetch result blocks consumed by
	// completed code execution calls are included in the response.
	// One of "full" or "excluded". Defaults to "full".
	ResponseInclusion string
}

// WebFetchResult20260318 represents the result of a web_fetch_20260318 tool call.
type WebFetchResult20260318 struct {
	// Type is always "web_fetch_result"
	Type string `json:"type"`

	// URL of the fetched resource
	URL string `json:"url"`

	// Content holds the fetched document content
	Content WebFetchDocument20260209 `json:"content"`

	// RetrievedAt is an ISO 8601 timestamp when the content was retrieved (may be nil)
	RetrievedAt *string `json:"retrievedAt"`
}

// webFetch20260318Opts stores the tool configuration and implements ToAnthropicAPIMap
// for the Anthropic tool converter.
type webFetch20260318Opts struct {
	Config WebFetch20260318Config
}

// ToAnthropicAPIMap returns the Anthropic API representation of this tool.
// Called by the Anthropic provider's tool converter. Note there is no beta
// header for web_fetch_20260318 (TS anthropic-prepare-tools.ts).
func (o *webFetch20260318Opts) ToAnthropicAPIMap() map[string]interface{} {
	m := map[string]interface{}{
		"type": "web_fetch_20260318",
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
	if o.Config.UseCache != nil {
		m["use_cache"] = *o.Config.UseCache
	}
	if o.Config.ResponseInclusion != "" {
		m["response_inclusion"] = o.Config.ResponseInclusion
	}
	return m
}

// ParseWebFetch20260318Result parses a JSON-encoded web_fetch_result object into a
// typed WebFetchResult20260318 struct.
//
// Use this to decode the tool result returned when Claude calls web_fetch_20260318.
// Check Source.IsPDF() or Source.IsPlainText() to determine how to process the content.
func ParseWebFetch20260318Result(data []byte) (*WebFetchResult20260318, error) {
	var result WebFetchResult20260318
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("web_fetch_20260318 result parsing: %w", err)
	}
	return &result, nil
}

// WebFetch20260318 creates an Anthropic web fetch provider tool (version 2026-03-18).
//
// This tool enables Claude to fetch and read content from web URLs. The fetch is
// executed by Anthropic's servers automatically — no local execution required.
//
// The tool supports fetching both base64-encoded PDF documents and plain text content.
// Use ParseWebFetch20260318Result to decode tool results into typed structs.
//
// Tool ID: "anthropic.web_fetch_20260318"
//
// Example:
//
//	maxTokens := 8192
//	fetchTool := tools.WebFetch20260318(tools.WebFetch20260318Config{
//	    MaxContentTokens: &maxTokens,
//	    Citations: &tools.WebFetchCitations{Enabled: true},
//	    AllowedDomains: []string{"docs.anthropic.com"},
//	})
func WebFetch20260318(config WebFetch20260318Config) types.Tool {
	return types.Tool{
		Name:        "anthropic.web_fetch_20260318",
		Description: "Fetch and read content from a URL (Anthropic web fetch, version 2026-03-18). Returns PDF or plain text content. Executed by Anthropic's servers.",
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
			return nil, fmt.Errorf("web_fetch_20260318 is executed by the Anthropic provider, not locally")
		},
		ProviderExecuted:        true,
		SupportsDeferredResults: true,
		ProviderOptions:         &webFetch20260318Opts{Config: config},
	}
}
