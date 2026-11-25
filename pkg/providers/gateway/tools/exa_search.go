package tools

import (
	"context"
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type ExaSearchConfig struct {
	Type               string
	NumResults         *int
	Category           string
	UserLocation       string
	IncludeDomains     []string
	ExcludeDomains     []string
	StartPublishedDate string
	EndPublishedDate   string
	Contents           *ExaSearchContentsConfig
}

type ExaSearchContentsConfig struct {
	Text             interface{}
	Highlights       interface{}
	MaxAgeHours      *int
	LivecrawlTimeout *int
	Subpages         *int
	SubpageTarget    interface{}
	Extras           *ExaSearchExtrasConfig
}

type ExaSearchTextConfig struct {
	MaxCharacters   *int     `json:"maxCharacters,omitempty"`
	IncludeHTMLTags *bool    `json:"includeHtmlTags,omitempty"`
	Verbosity       string   `json:"verbosity,omitempty"`
	IncludeSections []string `json:"includeSections,omitempty"`
	ExcludeSections []string `json:"excludeSections,omitempty"`
}

type ExaSearchHighlightsConfig struct {
	Query         string `json:"query,omitempty"`
	MaxCharacters *int   `json:"maxCharacters,omitempty"`
}

type ExaSearchExtrasConfig struct {
	Links      *int
	ImageLinks *int
}

type ExaSearchTool types.Tool

func NewExaSearch(config ExaSearchConfig) ExaSearchTool {
	properties := map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": "Natural-language web search query. This is required.",
		},
		"type": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"auto", "fast", "instant"},
			"description": "Search method. Use auto for the default balance of speed and quality.",
		},
		"num_results": map[string]interface{}{
			"type":        "number",
			"description": "Maximum number of results to return (1-100, default: 10).",
		},
		"category": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"company", "people", "research paper", "news", "personal site", "financial report"},
			"description": "Optional content category to focus results.",
		},
		"user_location": map[string]interface{}{
			"type":        "string",
			"description": "Two-letter ISO country code such as 'US'.",
		},
		"include_domains": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Only return results from these domains.",
		},
		"exclude_domains": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Exclude results from these domains.",
		},
		"start_published_date": map[string]interface{}{
			"type":        "string",
			"description": "Only return links published after this ISO 8601 date.",
		},
		"end_published_date": map[string]interface{}{
			"type":        "string",
			"description": "Only return links published before this ISO 8601 date.",
		},
		"contents": exaContentsSchema(),
	}

	tool := types.Tool{
		Name: "exa_search",
		Description: "Search the web using Exa for current information and token-efficient excerpts optimized for agent workflows. " +
			"Supports search type, category, domain, date, location, and content extraction controls.",
		Parameters:       map[string]interface{}{"type": "object", "properties": properties, "required": []string{"query"}},
		OutputSchema:     exaSearchOutputSchema(),
		ProviderExecuted: true,
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       "gateway.exa_search",
		ProviderArgs:     exaSearchArgs(config),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "exa_search",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}
	return ExaSearchTool(tool)
}

// exaSearchOutputSchemaJSON is the TS output schema (packages/gateway/src/tool/exa-search.ts) as zod's
// toJSONSchema renders it (draft-07, io "input"). ValidateUIMessages checks
// tool outputs against it, so nullable fields are anyOf [T, null] and the
// required lists match zod's non-optional fields.
const exaSearchOutputSchemaJSON = `{
	"anyOf": [
		{
			"type": "object",
			"properties": {
				"requestId": {
					"type": "string"
				},
				"searchType": {
					"type": "string"
				},
				"resolvedSearchType": {
					"type": "string"
				},
				"results": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"title": {
								"type": "string"
							},
							"url": {
								"type": "string"
							},
							"id": {
								"type": "string"
							},
							"publishedDate": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"author": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"image": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"favicon": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"text": {
								"type": "string"
							},
							"highlights": {
								"type": "array",
								"items": {
									"type": "string"
								}
							},
							"highlightScores": {
								"type": "array",
								"items": {
									"type": "number"
								}
							},
							"summary": {
								"type": "string"
							},
							"subpages": {
								"type": "array",
								"items": {}
							},
							"extras": {
								"type": "object",
								"properties": {
									"links": {
										"type": "array",
										"items": {
											"type": "string"
										}
									},
									"imageLinks": {
										"type": "array",
										"items": {
											"type": "string"
										}
									}
								}
							}
						},
						"required": [
							"title",
							"url",
							"id"
						]
					}
				},
				"costDollars": {
					"type": "object",
					"properties": {
						"total": {
							"type": "number"
						},
						"search": {
							"type": "object",
							"propertyNames": {
								"type": "string"
							},
							"additionalProperties": {
								"type": "number"
							}
						}
					}
				}
			},
			"required": [
				"requestId",
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
						"rate_limit",
						"timeout",
						"invalid_input",
						"configuration_error",
						"execution_error",
						"unknown"
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

func exaSearchOutputSchema() map[string]interface{} {
	return mustDecodeToolSchema(exaSearchOutputSchemaJSON)
}

func exaSearchArgs(config ExaSearchConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.Type != "" {
		args["type"] = config.Type
	}
	if config.NumResults != nil {
		args["numResults"] = *config.NumResults
	}
	if config.Category != "" {
		args["category"] = config.Category
	}
	if config.UserLocation != "" {
		args["userLocation"] = config.UserLocation
	}
	if config.IncludeDomains != nil {
		args["includeDomains"] = config.IncludeDomains
	}
	if config.ExcludeDomains != nil {
		args["excludeDomains"] = config.ExcludeDomains
	}
	if config.StartPublishedDate != "" {
		args["startPublishedDate"] = config.StartPublishedDate
	}
	if config.EndPublishedDate != "" {
		args["endPublishedDate"] = config.EndPublishedDate
	}
	if config.Contents != nil {
		args["contents"] = exaContentsArgs(*config.Contents)
	}
	return args
}

func exaContentsArgs(config ExaSearchContentsConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.Text != nil {
		args["text"] = config.Text
	}
	if config.Highlights != nil {
		args["highlights"] = config.Highlights
	}
	if config.MaxAgeHours != nil {
		args["maxAgeHours"] = *config.MaxAgeHours
	}
	if config.LivecrawlTimeout != nil {
		args["livecrawlTimeout"] = *config.LivecrawlTimeout
	}
	if config.Subpages != nil {
		args["subpages"] = *config.Subpages
	}
	if config.SubpageTarget != nil {
		args["subpageTarget"] = config.SubpageTarget
	}
	if config.Extras != nil {
		extras := map[string]interface{}{}
		if config.Extras.Links != nil {
			extras["links"] = *config.Extras.Links
		}
		if config.Extras.ImageLinks != nil {
			extras["imageLinks"] = *config.Extras.ImageLinks
		}
		args["extras"] = extras
	}
	return args
}

func exaContentsSchema() map[string]interface{} {
	sections := []string{"header", "navigation", "banner", "body", "sidebar", "footer", "metadata"}
	return map[string]interface{}{
		"type":        "object",
		"description": "Controls extracted page content and freshness.",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"anyOf": []interface{}{
					map[string]interface{}{"type": "boolean"},
					map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"max_characters":    map[string]interface{}{"type": "number"},
							"include_html_tags": map[string]interface{}{"type": "boolean"},
							"verbosity":         map[string]interface{}{"type": "string", "enum": []string{"compact", "standard", "full"}},
							"include_sections":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string", "enum": sections}},
							"exclude_sections":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string", "enum": sections}},
						},
					},
				},
			},
			"highlights": map[string]interface{}{
				"anyOf": []interface{}{
					map[string]interface{}{"type": "boolean"},
					map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}, "max_characters": map[string]interface{}{"type": "number"}}},
				},
			},
			"max_age_hours":     map[string]interface{}{"type": "number"},
			"livecrawl_timeout": map[string]interface{}{"type": "number"},
			"subpages":          map[string]interface{}{"type": "number"},
			"subpage_target":    map[string]interface{}{"anyOf": []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}}},
			"extras":            map[string]interface{}{"type": "object", "properties": map[string]interface{}{"links": map[string]interface{}{"type": "number"}, "image_links": map[string]interface{}{"type": "number"}}},
		},
	}
}

func (t ExaSearchTool) ToTool() types.Tool {
	return types.Tool(t)
}

// mustDecodeToolSchema decodes a JSON schema constant into a fresh map, so
// callers that change one tool's schema do not affect another tool's.
func mustDecodeToolSchema(src string) map[string]interface{} {
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(src), &out); err != nil {
		panic("gateway tools: invalid schema constant: " + err.Error())
	}
	return out
}
