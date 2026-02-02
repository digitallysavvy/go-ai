package tools

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// PerplexitySearchConfig contains configuration for the Perplexity Search tool
type PerplexitySearchConfig struct {
	// MaxResults is the default maximum number of search results to return (1-20, default: 10)
	MaxResults int

	// MaxTokensPerPage is the default maximum tokens to extract per search result page (256-2048, default: 2048)
	MaxTokensPerPage int

	// MaxTokens is the default maximum total tokens across all search results (default: 25000, max: 1000000)
	MaxTokens int

	// Country is the default two-letter ISO 3166-1 alpha-2 country code for regional search results
	// Examples: 'US', 'GB', 'FR'
	Country string

	// SearchDomainFilter is the default list of domains to include or exclude from search results (max 20)
	// To include: []string{"nature.com", "science.org"}
	// To exclude: []string{"-example.com", "-spam.net"}
	SearchDomainFilter []string

	// SearchLanguageFilter is the default list of ISO 639-1 language codes to filter results (max 10, lowercase)
	// Examples: []string{"en", "fr", "de"}
	SearchLanguageFilter []string

	// SearchRecencyFilter is the default recency filter for results
	// Cannot be combined with searchAfterDate/searchBeforeDate at runtime
	// Options: "day", "week", "month", "year"
	SearchRecencyFilter string
}

// PerplexitySearchTool is the return type for the perplexity search tool factory
type PerplexitySearchTool types.Tool

// NewPerplexitySearch creates a perplexity search tool with the given configuration
func NewPerplexitySearch(config PerplexitySearchConfig) PerplexitySearchTool {
	// Build parameters schema
	properties := map[string]interface{}{
		"query": map[string]interface{}{
			"description": "Search query (string) or multiple queries (array of up to 5 strings). Multi-query searches return combined results from all queries.",
			"oneOf": []map[string]interface{}{
				{"type": "string"},
				{
					"type":     "array",
					"items":    map[string]interface{}{"type": "string"},
					"maxItems": 5,
				},
			},
		},
		"max_results": map[string]interface{}{
			"type":        "integer",
			"description": "Maximum number of search results to return (1-20, default: 10)",
			"minimum":     1,
			"maximum":     20,
		},
		"max_tokens_per_page": map[string]interface{}{
			"type":        "integer",
			"description": "Maximum number of tokens to extract per search result page (256-2048, default: 2048)",
			"minimum":     256,
			"maximum":     2048,
		},
		"max_tokens": map[string]interface{}{
			"type":        "integer",
			"description": "Maximum total tokens across all search results (default: 25000, max: 1000000)",
			"maximum":     1000000,
		},
		"country": map[string]interface{}{
			"type":        "string",
			"description": "Two-letter ISO 3166-1 alpha-2 country code for regional search results (e.g., 'US', 'GB', 'FR')",
		},
		"search_domain_filter": map[string]interface{}{
			"type":        "array",
			"description": "List of domains to include or exclude from search results (max 20). To include: ['nature.com', 'science.org']. To exclude: ['-example.com', '-spam.net']",
			"items":       map[string]interface{}{"type": "string"},
			"maxItems":    20,
		},
		"search_language_filter": map[string]interface{}{
			"type":        "array",
			"description": "List of ISO 639-1 language codes to filter results (max 10, lowercase). Examples: ['en', 'fr', 'de']",
			"items":       map[string]interface{}{"type": "string"},
			"maxItems":    10,
		},
		"search_after_date": map[string]interface{}{
			"type":        "string",
			"description": "Include only results published after this date. Format: 'MM/DD/YYYY' (e.g., '3/1/2025'). Cannot be used with search_recency_filter.",
		},
		"search_before_date": map[string]interface{}{
			"type":        "string",
			"description": "Include only results published before this date. Format: 'MM/DD/YYYY' (e.g., '3/15/2025'). Cannot be used with search_recency_filter.",
		},
		"last_updated_after_filter": map[string]interface{}{
			"type":        "string",
			"description": "Include only results last updated after this date. Format: 'MM/DD/YYYY' (e.g., '3/1/2025'). Cannot be used with search_recency_filter.",
		},
		"last_updated_before_filter": map[string]interface{}{
			"type":        "string",
			"description": "Include only results last updated before this date. Format: 'MM/DD/YYYY' (e.g., '3/15/2025'). Cannot be used with search_recency_filter.",
		},
		"search_recency_filter": map[string]interface{}{
			"type":        "string",
			"description": "Filter results by relative time period. Cannot be used with search_after_date or search_before_date.",
			"enum":        []string{"day", "week", "month", "year"},
		},
	}

	parameters := map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   []string{"query"},
	}

	tool := types.Tool{
		Name: "gateway.perplexity_search",
		Description: "Search the web using Perplexity's Search API for real-time information, news, research papers, and articles. " +
			"Provides ranked search results with advanced filtering options including domain, language, date range, and recency filters.",
		Title:            "Perplexity Search",
		Parameters:       parameters,
		ProviderExecuted: true,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			// This should never be called for provider-executed tools
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "gateway.perplexity_search",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}

	return PerplexitySearchTool(tool)
}

// ToTool converts the PerplexitySearchTool to a types.Tool
func (t PerplexitySearchTool) ToTool() types.Tool {
	return types.Tool(t)
}
