package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// WebFetch20260209Config configures the web_fetch_20260209 provider tool.
type WebFetch20260209Config struct {
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
}

// WebFetchCitations controls citation behavior for web_fetch_20260209.
type WebFetchCitations struct {
	// Enabled enables Claude to cite specific passages from fetched documents.
	Enabled bool `json:"enabled"`
}

// WebFetchResult20260209 represents the result of a web_fetch_20260209 tool call.
type WebFetchResult20260209 struct {
	// Type is always "web_fetch_result"
	Type string `json:"type"`

	// URL of the fetched resource
	URL string `json:"url"`

	// Content holds the fetched document content
	Content WebFetchDocument20260209 `json:"content"`

	// RetrievedAt is an ISO 8601 timestamp when the content was retrieved (may be nil)
	RetrievedAt *string `json:"retrievedAt"`
}

// WebFetchDocument20260209 contains the document content returned by web_fetch_20260209.
type WebFetchDocument20260209 struct {
	// Type is always "document"
	Type string `json:"type"`

	// Title of the document (may be nil)
	Title *string `json:"title"`

	// Citations is present when citations were enabled (optional)
	Citations *WebFetchCitations `json:"citations,omitempty"`

	// Source contains the actual content, either as a base64-encoded PDF or plain text.
	// Use IsPDF() or IsPlainText() on Source to determine the content type.
	Source WebFetchSource `json:"source"`
}

// WebFetchSource is a discriminated union representing the content of a fetched document.
// The Type field distinguishes between base64-encoded PDF ("base64") and plain text ("text").
//
// Use IsPDF() to check for PDF content and IsPlainText() for plain text content.
type WebFetchSource struct {
	// Type is "base64" for PDF content or "text" for plain text.
	Type string `json:"type"`

	// MediaType is "application/pdf" for PDFs or "text/plain" for plain text.
	MediaType string `json:"mediaType"`

	// Data holds the content: base64-encoded bytes for PDFs, raw text for plain text.
	Data string `json:"data"`
}

// IsPDF returns true when this source contains a base64-encoded PDF document.
func (s WebFetchSource) IsPDF() bool {
	return s.Type == "base64"
}

// IsPlainText returns true when this source contains plain text content.
func (s WebFetchSource) IsPlainText() bool {
	return s.Type == "text"
}

// webFetch20260209Opts stores the tool configuration and implements ToAnthropicAPIMap
// for the Anthropic tool converter.
type webFetch20260209Opts struct {
	Config WebFetch20260209Config
}

// ToAnthropicAPIMap returns the Anthropic API representation of this tool.
// Called by the Anthropic provider's tool converter.
func (o *webFetch20260209Opts) ToAnthropicAPIMap() map[string]interface{} {
	m := map[string]interface{}{
		"type": "web_fetch_20260209",
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

// ParseWebFetchResult parses a JSON-encoded web_fetch_result object into a typed
// WebFetchResult20260209 struct.
//
// Use this to decode the tool result returned when Claude calls web_fetch_20260209.
// Check Source.IsPDF() or Source.IsPlainText() to determine how to process the content.
func ParseWebFetchResult(data []byte) (*WebFetchResult20260209, error) {
	var result WebFetchResult20260209
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("web_fetch_20260209 result parsing: %w", err)
	}
	return &result, nil
}

// WebFetch20260209 creates an Anthropic web fetch provider tool (version 2026-02-09).
//
// This tool enables Claude to fetch and read content from web URLs. The fetch is
// executed by Anthropic's servers automatically — no local execution required.
//
// The tool supports fetching both base64-encoded PDF documents and plain text content.
// Use ParseWebFetchResult to decode tool results into typed structs.
//
// Tool ID: "anthropic.web_fetch_20260209"
//
// Example:
//
//	maxTokens := 8192
//	fetchTool := tools.WebFetch20260209(tools.WebFetch20260209Config{
//	    MaxContentTokens: &maxTokens,
//	    Citations: &tools.WebFetchCitations{Enabled: true},
//	    AllowedDomains: []string{"docs.anthropic.com"},
//	})
func WebFetch20260209(config WebFetch20260209Config) types.Tool {
	return types.Tool{
		Name:        "anthropic.web_fetch_20260209",
		Description: "Fetch and read content from a URL (Anthropic web fetch, version 2026-02-09). Returns PDF or plain text content. Executed by Anthropic's servers.",
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
			return nil, fmt.Errorf("web_fetch_20260209 is executed by the Anthropic provider, not locally")
		},
		ProviderExecuted:        true,
		SupportsDeferredResults: true,
		ProviderOptions:         &webFetch20260209Opts{Config: config},
	}
}
