package tools

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ParallelSearchConfig contains configuration for the Parallel Search tool
type ParallelSearchConfig struct {
	// Mode preset for different use cases:
	// - "one-shot": Comprehensive results with longer excerpts for single-response answers (default)
	// - "agentic": Concise, token-efficient results for multi-step agentic workflows
	Mode string

	// MaxResults is the default maximum number of results to return (1-20)
	// Defaults to 10 if not specified
	MaxResults int

	// SourcePolicy is the default source policy for controlling which domains to include/exclude
	SourcePolicy *ParallelSearchSourcePolicy

	// Excerpts is the default excerpt configuration for controlling result length
	Excerpts *ParallelSearchExcerpts

	// FetchPolicy is the default fetch policy for controlling content freshness
	FetchPolicy *ParallelSearchFetchPolicy
}

// ParallelSearchSourcePolicy controls which domains to include/exclude and freshness
type ParallelSearchSourcePolicy struct {
	// IncludeDomains is a list of domains to include in search results
	// Example: []string{"wikipedia.org", "nature.com"}
	IncludeDomains []string

	// ExcludeDomains is a list of domains to exclude from search results
	// Example: []string{"reddit.com", "twitter.com"}
	ExcludeDomains []string

	// AfterDate only includes results published after this date (ISO 8601 format)
	// Example: "2024-01-01"
	AfterDate string
}

// ParallelSearchExcerpts controls the length of result excerpts
type ParallelSearchExcerpts struct {
	// MaxCharsPerResult is the maximum characters per result
	MaxCharsPerResult int

	// MaxCharsTotal is the maximum total characters across all results
	MaxCharsTotal int
}

// ParallelSearchFetchPolicy controls content freshness
type ParallelSearchFetchPolicy struct {
	// MaxAgeSeconds is the maximum age in seconds for cached content
	// Set to 0 to always fetch fresh content
	MaxAgeSeconds int
}

// ParallelSearchTool is the return type for the parallel search tool factory
type ParallelSearchTool types.Tool

// NewParallelSearch creates a parallel search tool with the given configuration
func NewParallelSearch(config ParallelSearchConfig) ParallelSearchTool {
	// Build parameters schema
	properties := map[string]interface{}{
		"objective": map[string]interface{}{
			"type":        "string",
			"description": "Natural-language description of the web research goal, including source or freshness guidance and broader context from the task. Maximum 5000 characters.",
		},
		"search_queries": map[string]interface{}{
			"type":        "array",
			"description": "Optional search queries to supplement the objective. Maximum 200 characters per query.",
			"items": map[string]interface{}{
				"type": "string",
			},
		},
		"mode": map[string]interface{}{
			"type":        "string",
			"description": "Mode preset: 'one-shot' for comprehensive results with longer excerpts (default), 'agentic' for concise, token-efficient results for multi-step workflows.",
			"enum":        []string{"one-shot", "agentic"},
		},
		"max_results": map[string]interface{}{
			"type":        "integer",
			"description": "Maximum number of results to return (1-20). Defaults to 10 if not specified.",
			"minimum":     1,
			"maximum":     20,
		},
	}

	// Add source_policy if configured
	if config.SourcePolicy != nil {
		properties["source_policy"] = map[string]interface{}{
			"type":        "object",
			"description": "Source policy for controlling which domains to include/exclude and freshness.",
			"properties": map[string]interface{}{
				"include_domains": map[string]interface{}{
					"type":        "array",
					"description": "List of domains to include in search results.",
					"items":       map[string]interface{}{"type": "string"},
				},
				"exclude_domains": map[string]interface{}{
					"type":        "array",
					"description": "List of domains to exclude from search results.",
					"items":       map[string]interface{}{"type": "string"},
				},
				"after_date": map[string]interface{}{
					"type":        "string",
					"description": "Only include results published after this date (ISO 8601 format).",
				},
			},
		}
	}

	// Add excerpts if configured
	if config.Excerpts != nil {
		properties["excerpts"] = map[string]interface{}{
			"type":        "object",
			"description": "Excerpt configuration for controlling result length.",
			"properties": map[string]interface{}{
				"max_chars_per_result": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum characters per result.",
				},
				"max_chars_total": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum total characters across all results.",
				},
			},
		}
	}

	// Add fetch_policy if configured
	if config.FetchPolicy != nil {
		properties["fetch_policy"] = map[string]interface{}{
			"type":        "object",
			"description": "Fetch policy for controlling content freshness.",
			"properties": map[string]interface{}{
				"max_age_seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum age in seconds for cached content. Set to 0 to always fetch fresh content.",
				},
			},
		}
	}

	parameters := map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   []string{"objective"},
	}

	tool := types.Tool{
		Name: "gateway.parallel_search",
		Description: "Search the web using Parallel AI's Search API for LLM-optimized excerpts. " +
			"Takes a natural language objective and returns relevant excerpts, " +
			"replacing multiple keyword searches with a single call for broad or complex queries. " +
			"Supports different search types for depth vs breadth tradeoffs.",
		Title:            "Parallel Search",
		Parameters:       parameters,
		ProviderExecuted: true,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			// This should never be called for provider-executed tools
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "gateway.parallel_search",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}

	return ParallelSearchTool(tool)
}

// ToTool converts the ParallelSearchTool to a types.Tool
func (t ParallelSearchTool) ToTool() types.Tool {
	return types.Tool(t)
}
