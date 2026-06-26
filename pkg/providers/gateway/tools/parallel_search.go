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

	// MaxResults is the default maximum number of results to return (1-20).
	// Nil omits the value; a non-nil pointer serializes the value, including 0.
	MaxResults *int

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
	MaxCharsPerResult *int

	// MaxCharsTotal is the maximum total characters across all results
	MaxCharsTotal *int
}

// ParallelSearchFetchPolicy controls content freshness
type ParallelSearchFetchPolicy struct {
	// MaxAgeSeconds is the maximum age in seconds for cached content
	// Set to 0 to always fetch fresh content
	MaxAgeSeconds *int
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
			"type":        "number",
			"description": "Maximum number of results to return (1-20). Defaults to 10 if not specified.",
		},
	}

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

	properties["excerpts"] = map[string]interface{}{
		"type":        "object",
		"description": "Excerpt configuration for controlling result length.",
		"properties": map[string]interface{}{
			"max_chars_per_result": map[string]interface{}{
				"type":        "number",
				"description": "Maximum characters per result.",
			},
			"max_chars_total": map[string]interface{}{
				"type":        "number",
				"description": "Maximum total characters across all results.",
			},
		},
	}

	properties["fetch_policy"] = map[string]interface{}{
		"type":        "object",
		"description": "Fetch policy for controlling content freshness.",
		"properties": map[string]interface{}{
			"max_age_seconds": map[string]interface{}{
				"type":        "number",
				"description": "Maximum age in seconds for cached content. Set to 0 to always fetch fresh content.",
			},
		},
	}

	parameters := map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   []string{"objective"},
	}

	tool := types.Tool{
		Name: "parallel_search",
		Description: "Search the web using Parallel AI's Search API for LLM-optimized excerpts. " +
			"Takes a natural language objective and returns relevant excerpts, " +
			"replacing multiple keyword searches with a single call for broad or complex queries. " +
			"Supports different search types for depth vs breadth tradeoffs.",
		Title:            "Parallel Search",
		Parameters:       parameters,
		OutputSchema:     parallelSearchOutputSchema(),
		ProviderExecuted: true,
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       "gateway.parallel_search",
		ProviderArgs:     parallelSearchArgs(config),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			// This should never be called for provider-executed tools
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "parallel_search",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}

	return ParallelSearchTool(tool)
}

func parallelSearchOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"oneOf": []interface{}{
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"searchId": map[string]interface{}{"type": "string"},
					"results": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"url":            map[string]interface{}{"type": "string"},
								"title":          map[string]interface{}{"type": "string"},
								"excerpt":        map[string]interface{}{"type": "string"},
								"publishDate":    map[string]interface{}{"type": "string", "nullable": true},
								"relevanceScore": map[string]interface{}{"type": "number"},
							},
						},
					},
				},
			},
			searchErrorSchema([]string{"api_error", "rate_limit", "timeout", "invalid_input", "configuration_error", "unknown"}),
		},
	}
}

func parallelSearchArgs(config ParallelSearchConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.Mode != "" {
		args["mode"] = config.Mode
	}
	if config.MaxResults != nil {
		args["maxResults"] = *config.MaxResults
	}
	if config.SourcePolicy != nil {
		sourcePolicy := map[string]interface{}{}
		if len(config.SourcePolicy.IncludeDomains) > 0 {
			sourcePolicy["includeDomains"] = config.SourcePolicy.IncludeDomains
		}
		if len(config.SourcePolicy.ExcludeDomains) > 0 {
			sourcePolicy["excludeDomains"] = config.SourcePolicy.ExcludeDomains
		}
		if config.SourcePolicy.AfterDate != "" {
			sourcePolicy["afterDate"] = config.SourcePolicy.AfterDate
		}
		args["sourcePolicy"] = sourcePolicy
	}
	if config.Excerpts != nil {
		excerpts := map[string]interface{}{}
		if config.Excerpts.MaxCharsPerResult != nil {
			excerpts["maxCharsPerResult"] = *config.Excerpts.MaxCharsPerResult
		}
		if config.Excerpts.MaxCharsTotal != nil {
			excerpts["maxCharsTotal"] = *config.Excerpts.MaxCharsTotal
		}
		args["excerpts"] = excerpts
	}
	if config.FetchPolicy != nil {
		fetchPolicy := map[string]interface{}{}
		if config.FetchPolicy.MaxAgeSeconds != nil {
			fetchPolicy["maxAgeSeconds"] = *config.FetchPolicy.MaxAgeSeconds
		}
		args["fetchPolicy"] = fetchPolicy
	}
	return args
}

// ToTool converts the ParallelSearchTool to a types.Tool
func (t ParallelSearchTool) ToTool() types.Tool {
	return types.Tool(t)
}
