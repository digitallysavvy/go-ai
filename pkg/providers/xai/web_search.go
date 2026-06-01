package xai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// WebSearchConfig contains configuration for the xAI WebSearch tool.
type WebSearchConfig struct {
	// AllowedDomains limits search results to specific domains.
	AllowedDomains []string

	// ExcludedDomains omits results from specific domains.
	ExcludedDomains []string

	// EnableImageSearch enables image search in xAI Responses web search.
	EnableImageSearch *bool

	// EnableImageUnderstanding enables image understanding in search results.
	EnableImageUnderstanding *bool
}

// WebSearch creates a provider-executed tool that searches the web for current information.
// Search is handled by xAI's servers and returns queries and source results.
//
// Example:
//
//	tool := xai.WebSearch(xai.WebSearchConfig{
//	    AllowedDomains: []string{"example.com"},
//	})
func WebSearch(config WebSearchConfig) types.Tool {
	return types.Tool{
		Name:             "xai.web_search",
		Description:      "Search the web for current information. Returns queries and source results.",
		ProviderExecuted: true,
		ProviderOptions:  config,
		Execute:          providerExecutedNoop("xai.web_search"),
	}
}
