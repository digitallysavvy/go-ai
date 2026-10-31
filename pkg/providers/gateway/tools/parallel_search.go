package tools

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Parallel Search modes. "turbo" and "fast" trade excerpt depth for latency
// ("turbo" is English and Japanese only), "basic" is balanced and "advanced"
// is the most thorough.
const (
	ParallelSearchModeTurbo    = "turbo"
	ParallelSearchModeFast     = "fast"
	ParallelSearchModeBasic    = "basic"
	ParallelSearchModeAdvanced = "advanced"

	// ParallelSearchModeOneShot is a deprecated alias of "basic".
	//
	// Deprecated: use ParallelSearchModeBasic.
	ParallelSearchModeOneShot = "one-shot"
	// ParallelSearchModeAgentic is a deprecated alias of "advanced".
	//
	// Deprecated: use ParallelSearchModeAdvanced.
	ParallelSearchModeAgentic = "agentic"
)

// parallelSearchModes lists every mode the gateway input schema accepts.
var parallelSearchModes = []string{"turbo", "fast", "basic", "advanced", "one-shot", "agentic"}

// ParallelSearchConfig contains configuration for the Parallel Search tool
type ParallelSearchConfig struct {
	// Mode is the search mode: "turbo", "fast", "basic" (default), "advanced",
	// or the deprecated aliases "one-shot" (runs as "basic") and "agentic"
	// (runs as "advanced").
	Mode string

	// MaxResults is the default maximum number of results to return (1-20).
	// Nil omits the value; a non-nil pointer serializes the value, including 0.
	MaxResults *int

	// SourcePolicy is the default source policy for controlling which domains to include/exclude
	SourcePolicy *ParallelSearchSourcePolicy

	// Excerpts is the default excerpt configuration for controlling result length
	Excerpts *ParallelSearchExcerpts

	// MaxCharsTotal is the maximum total characters across all result excerpts.
	MaxCharsTotal *int

	// FetchPolicy is the default fetch policy for controlling content freshness
	FetchPolicy *ParallelSearchFetchPolicy

	// Location is an ISO 3166-1 alpha-2 country code to localize results,
	// e.g. "us". Invalid codes are ignored with a warning.
	Location string
}

// ParallelSearchSourcePolicy controls which domains to include/exclude and freshness
type ParallelSearchSourcePolicy struct {
	// IncludeDomains restricts results to these domains or domain/path
	// prefixes. When set, ExcludeDomains is ignored.
	// Example: []string{"wikipedia.org", "docs.python.org/3"}
	IncludeDomains []string

	// ExcludeDomains is a list of domains or domain/path prefixes to exclude
	// from results. Example: []string{"reddit.com"}
	ExcludeDomains []string

	// AfterDate only includes results published on or after this date (YYYY-MM-DD)
	// Example: "2024-01-01"
	AfterDate string
}

// ParallelSearchExcerpts controls the length of result excerpts
type ParallelSearchExcerpts struct {
	// MaxCharsPerResult is the maximum characters per result
	MaxCharsPerResult *int

	// MaxCharsTotal is the maximum total characters across all results.
	//
	// Deprecated: use ParallelSearchConfig.MaxCharsTotal instead.
	MaxCharsTotal *int
}

// ParallelSearchFetchPolicy controls content freshness
type ParallelSearchFetchPolicy struct {
	// MaxAgeSeconds is the maximum age in seconds of cached content before a
	// live fetch is attempted. Minimum 600; lower values are raised to 600.
	MaxAgeSeconds *int

	// TimeoutSeconds is the timeout in seconds for a live fetch when content
	// is not cached. The gateway caps it to stay within its own tool timeout.
	TimeoutSeconds *int

	// DisableCacheFallback returns an error instead of falling back to older
	// cached content when a live fetch fails or times out. Defaults to false.
	DisableCacheFallback *bool
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
			"description": "Provide 2-3 concise keyword queries, 3-6 words each (max 5, 200 chars each). Strongly recommended; the search runs on these queries.",
			"items": map[string]interface{}{
				"type": "string",
			},
		},
		"mode": map[string]interface{}{
			"type":        "string",
			"description": "Search mode: \"turbo\" and \"fast\" return quickly with shorter excerpts, \"basic\" is balanced (default), \"advanced\" is the most thorough. \"turbo\" supports English and Japanese only. \"one-shot\" and \"agentic\" are deprecated aliases of \"basic\" and \"advanced\".",
			"enum":        parallelSearchModes,
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
				"description": "Domains or domain/path prefixes to restrict results to, without scheme or port. Example: ['wikipedia.org', 'docs.python.org/3'].",
				"items":       map[string]interface{}{"type": "string"},
			},
			"exclude_domains": map[string]interface{}{
				"type":        "array",
				"description": "Domains or domain/path prefixes to exclude, without scheme or port. Example: ['reddit.com'].",
				"items":       map[string]interface{}{"type": "string"},
			},
			"after_date": map[string]interface{}{
				"type":        "string",
				"description": "Only include results published on or after this date. Use an ISO 8601 calendar date formatted YYYY-MM-DD (e.g. 2025-01-01); do not include a time.",
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
				"description": "Deprecated: use the top-level max_chars_total.",
			},
		},
	}

	properties["max_chars_total"] = map[string]interface{}{
		"type":        "number",
		"description": "Maximum total characters across all result excerpts.",
	}

	properties["fetch_policy"] = map[string]interface{}{
		"type":        "object",
		"description": "Fetch policy for controlling content freshness.",
		"properties": map[string]interface{}{
			"max_age_seconds": map[string]interface{}{
				"type":        "number",
				"description": "Maximum age of cached content in seconds before a live fetch, minimum 600.",
			},
			"timeout_seconds": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds for a live fetch when content is not cached. Capped by the gateway tool timeout.",
			},
			"disable_cache_fallback": map[string]interface{}{
				"type":        "boolean",
				"description": "Return an error instead of older cached content when a live fetch fails.",
			},
		},
	}

	properties["location"] = map[string]interface{}{
		"type":        "string",
		"description": "ISO 3166-1 alpha-2 country code to localize results, e.g. \"us\".",
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

// parallelSearchOutputSchemaJSON is the TS output schema (packages/gateway/src/tool/parallel-search.ts) as zod's
// toJSONSchema renders it (draft-07, io "input"). ValidateUIMessages checks
// tool outputs against it, so nullable fields are anyOf [T, null] and the
// required lists match zod's non-optional fields.
const parallelSearchOutputSchemaJSON = `{
	"anyOf": [
		{
			"type": "object",
			"properties": {
				"search_id": {
					"type": "string"
				},
				"results": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"url": {
								"type": "string"
							},
							"title": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"publish_date": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"excerpts": {
								"type": "array",
								"items": {
									"type": "string"
								}
							}
						},
						"required": [
							"url",
							"excerpts"
						]
					}
				},
				"warnings": {
					"anyOf": [
						{
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"type": {
										"type": "string"
									},
									"message": {
										"type": "string"
									},
									"detail": {
										"anyOf": [
											{
												"type": "object",
												"propertyNames": {
													"type": "string"
												},
												"additionalProperties": {}
											},
											{
												"type": "null"
											}
										]
									}
								},
								"required": [
									"type",
									"message"
								]
							}
						},
						{
							"type": "null"
						}
					]
				},
				"usage": {
					"anyOf": [
						{
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"name": {
										"type": "string"
									},
									"count": {
										"type": "number"
									}
								},
								"required": [
									"name"
								]
							}
						},
						{
							"type": "null"
						}
					]
				},
				"session_id": {
					"type": "string"
				}
			},
			"required": [
				"search_id",
				"results"
			]
		},
		{
			"type": "object",
			"properties": {
				"error": {
					"type": "string",
					"enum": [
						"api_error",
						"configuration_error",
						"execution_error",
						"gateway_rate_limit",
						"invalid_input",
						"rate_limit",
						"timeout",
						"unknown",
						"unknown_tool"
					]
				},
				"statusCode": {
					"type": "number"
				},
				"message": {
					"type": "string"
				}
			},
			"required": [
				"error",
				"message"
			]
		}
	]
}`

func parallelSearchOutputSchema() map[string]interface{} {
	return mustDecodeToolSchema(parallelSearchOutputSchemaJSON)
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
	if config.MaxCharsTotal != nil {
		args["maxCharsTotal"] = *config.MaxCharsTotal
	}
	if config.Location != "" {
		args["location"] = config.Location
	}
	if config.FetchPolicy != nil {
		fetchPolicy := map[string]interface{}{}
		if config.FetchPolicy.MaxAgeSeconds != nil {
			fetchPolicy["maxAgeSeconds"] = *config.FetchPolicy.MaxAgeSeconds
		}
		if config.FetchPolicy.TimeoutSeconds != nil {
			fetchPolicy["timeoutSeconds"] = *config.FetchPolicy.TimeoutSeconds
		}
		if config.FetchPolicy.DisableCacheFallback != nil {
			fetchPolicy["disableCacheFallback"] = *config.FetchPolicy.DisableCacheFallback
		}
		args["fetchPolicy"] = fetchPolicy
	}
	return args
}

// ToTool converts the ParallelSearchTool to a types.Tool
func (t ParallelSearchTool) ToTool() types.Tool {
	return types.Tool(t)
}
