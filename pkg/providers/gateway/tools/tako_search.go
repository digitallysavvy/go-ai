package tools

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TakoDataSourceConfig configures the curated Data Graph source for a Tako
// search (TS TakoDataSourceConfig).
type TakoDataSourceConfig struct {
	Count           *int
	IncludeContents *bool
	// Mode is the requested delivery mode for card data: "inline" or "url".
	Mode string
	// ContentFormat is the serialization for inlined card data:
	// "card_json", "csv", "json_compact", or "json_records".
	ContentFormat string
	MaxRows       *int
	NodeIDs       []string
	Strict        *bool
}

// TakoWebSourceConfig configures the web source for a Tako search (TS
// TakoWebSourceConfig).
type TakoWebSourceConfig struct {
	Count           *int
	IncludeContents *bool
	// Category is an optional web-result category filter: "finance",
	// "news", or "sports".
	Category               string
	IncludeDomains         []string
	ExcludeDomains         []string
	SnippetMaxChars        *int
	Highlights             *bool
	ArticleContentMaxChars *int
	PublishedAfter         string
	PublishedBefore        string
}

// TakoSearchSources selects which Tako sources to search (TS
// TakoSearchConfig.sources).
type TakoSearchSources struct {
	Data *TakoDataSourceConfig
	Web  *TakoWebSourceConfig
}

// TakoSearchLocation is end-user coordinates for localized results.
type TakoSearchLocation struct {
	Latitude  float64
	Longitude float64
}

// TakoSearchOutputSettings controls card rendering (TS
// TakoSearchConfig.outputSettings).
type TakoSearchOutputSettings struct {
	ImageDarkMode *bool
	// ForceRefresh only applies with effort "instant".
	ForceRefresh *bool
}

// TakoSearchConfig configures a Tako search provider-defined tool (TS
// TakoSearchConfig).
type TakoSearchConfig struct {
	// Effort is the search effort: "deep", "fast" (default), or "instant".
	Effort         string
	Sources        *TakoSearchSources
	Location       *TakoSearchLocation
	CountryCode    string
	Locale         string
	Timezone       string
	OutputSettings *TakoSearchOutputSettings
	// IncludeRelated is the maximum related search suggestions to include (1-20).
	IncludeRelated *int
}

// TakoSearchTool is the gateway.tako_search provider-defined tool.
type TakoSearchTool types.Tool

// NewTakoSearch creates a Tako search tool with the given configuration.
// It mirrors the TypeScript SDK's takoSearch/takoSearchToolFactory
// (packages/gateway/src/tool/tako-search.ts).
func NewTakoSearch(config TakoSearchConfig) TakoSearchTool {
	properties := map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": `Natural-language search query. Include the entity, metric, and time period. Quote a phrase to force it to one entity, for example "Tesla":PRODUCT price.`,
		},
		"effort": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"deep", "fast", "instant"},
			"description": "Search effort. fast is the balanced default, instant favors cached results and low latency, and deep broadens retrieval with reranking at higher cost and latency.",
		},
		"sources": map[string]interface{}{
			"type":        "object",
			"description": "Sources to search. Omit to search both curated data and the web. When provided, only keys present are searched.",
			"properties": map[string]interface{}{
				"data": takoDataSourceSchema(),
				"web":  takoWebSourceSchema(),
			},
		},
		"location": map[string]interface{}{
			"type":        "object",
			"description": "End-user coordinates for localized results.",
			"properties": map[string]interface{}{
				"latitude":  map[string]interface{}{"type": "number", "description": "Latitude between -90 and 90."},
				"longitude": map[string]interface{}{"type": "number", "description": "Longitude between -180 and 180."},
			},
			"required": []string{"latitude", "longitude"},
		},
		"country_code": map[string]interface{}{
			"type":        "string",
			"description": "Two-letter ISO 3166-1 country code, such as 'US'.",
		},
		"locale": map[string]interface{}{
			"type":        "string",
			"description": "BCP-47 locale, such as 'en-US'.",
		},
		"timezone": map[string]interface{}{
			"type":        "string",
			"description": "IANA timezone, such as 'America/New_York'.",
		},
		"output_settings": map[string]interface{}{
			"type":        "object",
			"description": "Controls card rendering in the search response.",
			"properties": map[string]interface{}{
				"image_dark_mode": map[string]interface{}{
					"type":        "boolean",
					"description": "Render card preview images in dark mode.",
				},
				"force_refresh": map[string]interface{}{
					"type":        "boolean",
					"description": "Instant-effort only. Request a refreshed instant result.",
				},
			},
		},
		"include_related": map[string]interface{}{
			"type":        "number",
			"description": "Maximum related search suggestions to include (1-20).",
		},
	}

	tool := types.Tool{
		Name: "tako_search",
		Description: "Search Tako's curated Data Graph and the web for entities, metrics, and time series, returning rendered " +
			"data cards and web results. Supports search effort, source selection, data/web filtering, and localization controls.",
		Title:            "Tako Search",
		Parameters:       map[string]interface{}{"type": "object", "properties": properties, "required": []string{"query"}},
		OutputSchema:     takoSearchOutputSchema(),
		ProviderExecuted: true,
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       "gateway.tako_search",
		ProviderArgs:     takoSearchArgs(config),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "tako_search",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}
	return TakoSearchTool(tool)
}

func takoDataSourceSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"count": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of data results to return (1-20). When include_contents is true, each additional result adds its own data surcharge.",
			},
			"include_contents": map[string]interface{}{
				"type":        "boolean",
				"description": "Inline rows for each data result. This adds a data surcharge based on row count and dataset source. To estimate cost, search with include_contents disabled and inspect cards.content.export_pricing. This applies to every returned card; limit sources.data.count and sources.data.max_rows to control cost.",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"inline", "url"},
				"description": "Requested data delivery mode. Search card data is always inline.",
			},
			"content_format": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"card_json", "csv", "json_compact", "json_records"},
				"description": "Serialization for inlined card data.",
			},
			"max_rows": map[string]interface{}{
				"type":        "number",
				"description": "Maximum rows to inline per result. Omit to use the allowance in cards.content.export_pricing. A data surcharge applies per 1,000 exported rows; lower values reduce cost.",
			},
			"node_ids": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Data Graph node IDs to prioritize. Maximum 20.",
			},
			"strict": map[string]interface{}{
				"type":        "boolean",
				"description": "Only return cards matching node_ids. Requires a non-empty node_ids.",
			},
		},
	}
}

func takoWebSourceSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"count": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of web results to return (1-20).",
			},
			"include_contents": map[string]interface{}{
				"type":        "boolean",
				"description": "Inline extracted web page text. This can add a data charge.",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"finance", "news", "sports"},
				"description": "Optional web-result category filter.",
			},
			"include_domains": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Only return results from these bare domains.",
			},
			"exclude_domains": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Exclude results from these bare domains.",
			},
			"snippet_max_chars": map[string]interface{}{
				"type":        "number",
				"description": "Maximum characters in each web-result snippet.",
			},
			"highlights": map[string]interface{}{
				"type":        "boolean",
				"description": "Include highlighted passages in web results. Defaults to true in AI Gateway.",
			},
			"article_content_max_chars": map[string]interface{}{
				"type":        "number",
				"description": "Maximum extracted characters per web page when including contents.",
			},
			"published_after": map[string]interface{}{
				"type":        "string",
				"description": "Keep results published on or after this ISO date (YYYY-MM-DD).",
			},
			"published_before": map[string]interface{}{
				"type":        "string",
				"description": "Keep results published on or before this ISO date (YYYY-MM-DD).",
			},
		},
	}
}

// takoSearchOutputSchema mirrors takoSearchOutputSchema in tako-search.ts.
// TS parity (5533946): the internal-only relevance_score and
// citation_number fields were dropped from the public output schema; they
// must never appear here even as passthrough hints.
// takoSearchOutputSchemaJSON is the TS output schema (packages/gateway/src/tool/tako-search.ts) as zod's
// toJSONSchema renders it (draft-07, io "input"). ValidateUIMessages checks
// tool outputs against it, so nullable fields are anyOf [T, null] and the
// required lists match zod's non-optional fields.
const takoSearchOutputSchemaJSON = `{
	"anyOf": [
		{
			"type": "object",
			"properties": {
				"request_id": {
					"type": "string"
				},
				"cards": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"card_id": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
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
							"description": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"semantic_description": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"webpage_url": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"image_url": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"embed_url": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"sources": {
								"anyOf": [
									{
										"type": "array",
										"items": {
											"type": "object",
											"properties": {
												"source_name": {
													"anyOf": [
														{
															"type": "string"
														},
														{
															"type": "null"
														}
													]
												},
												"source_description": {
													"anyOf": [
														{
															"type": "string"
														},
														{
															"type": "null"
														}
													]
												},
												"source_index": {
													"type": "string",
													"enum": [
														"data",
														"web"
													]
												},
												"source_text": {
													"anyOf": [
														{
															"type": "string"
														},
														{
															"type": "null"
														}
													]
												},
												"url": {
													"anyOf": [
														{
															"type": "string"
														},
														{
															"type": "null"
														}
													]
												}
											},
											"required": [
												"source_index"
											]
										}
									},
									{
										"type": "null"
									}
								]
							},
							"methodologies": {
								"anyOf": [
									{
										"type": "array",
										"items": {
											"type": "object",
											"properties": {
												"methodology_name": {
													"anyOf": [
														{
															"type": "string"
														},
														{
															"type": "null"
														}
													]
												},
												"methodology_description": {
													"anyOf": [
														{
															"type": "string"
														},
														{
															"type": "null"
														}
													]
												}
											},
											"required": [
												"methodology_name",
												"methodology_description"
											]
										}
									},
									{
										"type": "null"
									}
								]
							},
							"source_indexes": {
								"anyOf": [
									{
										"type": "array",
										"items": {
											"type": "string",
											"enum": [
												"data",
												"web"
											]
										}
									},
									{
										"type": "null"
									}
								]
							},
							"card_type": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"relevance": {
								"anyOf": [
									{
										"type": "string",
										"enum": [
											"High",
											"Low",
											"Medium"
										]
									},
									{
										"type": "null"
									}
								]
							},
							"content": {
								"anyOf": [
									{
										"type": "object",
										"properties": {
											"content_format": {
												"anyOf": [
													{
														"type": "string",
														"enum": [
															"card_json",
															"csv",
															"json_compact",
															"json_records"
														]
													},
													{
														"type": "null"
													}
												]
											},
											"cost": {
												"type": "number"
											},
											"data": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											},
											"records": {
												"anyOf": [
													{
														"type": "array",
														"items": {
															"type": "object",
															"propertyNames": {
																"type": "string"
															},
															"additionalProperties": {
																"anyOf": [
																	{
																		"anyOf": [
																			{
																				"type": "boolean"
																			},
																			{
																				"type": "number"
																			},
																			{
																				"type": "string"
																			}
																		]
																	},
																	{
																		"type": "null"
																	}
																]
															}
														}
													},
													{
														"type": "null"
													}
												]
											},
											"dataset": {
												"anyOf": [
													{
														"type": "object",
														"properties": {
															"columns": {
																"type": "array",
																"items": {
																	"type": "object",
																	"properties": {
																		"name": {
																			"type": "string"
																		},
																		"type": {
																			"type": "string",
																			"enum": [
																				"boolean",
																				"date",
																				"datetime",
																				"number",
																				"string"
																			]
																		},
																		"unit": {
																			"anyOf": [
																				{
																					"type": "string"
																				},
																				{
																					"type": "null"
																				}
																			]
																		}
																	},
																	"required": [
																		"name",
																		"type"
																	]
																}
															},
															"rows": {
																"type": "array",
																"items": {
																	"type": "array",
																	"items": {
																		"anyOf": [
																			{
																				"anyOf": [
																					{
																						"type": "boolean"
																					},
																					{
																						"type": "number"
																					},
																					{
																						"type": "string"
																					}
																				]
																			},
																			{
																				"type": "null"
																			}
																		]
																	}
																}
															},
															"total_rows": {
																"type": "number"
															},
															"truncated": {
																"type": "boolean"
															},
															"ref": {
																"type": "string"
															},
															"sources": {
																"type": "array",
																"items": {
																	"type": "object",
																	"properties": {
																		"name": {
																			"type": "string"
																		},
																		"index": {
																			"type": "string",
																			"enum": [
																				"data",
																				"web"
																			]
																		}
																	},
																	"required": [
																		"name"
																	]
																}
															},
															"provenance": {
																"type": "string",
																"enum": [
																	"query",
																	"web_extraction"
																]
															}
														},
														"required": [
															"columns",
															"rows",
															"total_rows",
															"truncated",
															"ref",
															"sources"
														]
													},
													{
														"type": "null"
													}
												]
											},
											"card_data": {
												"anyOf": [
													{
														"type": "object",
														"properties": {},
														"additionalProperties": {}
													},
													{
														"type": "null"
													}
												]
											},
											"card_data_schema": {
												"anyOf": [
													{
														"type": "object",
														"properties": {},
														"additionalProperties": {}
													},
													{
														"type": "null"
													}
												]
											},
											"url": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											},
											"expires_at": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											},
											"total_rows": {
												"anyOf": [
													{
														"type": "number"
													},
													{
														"type": "null"
													}
												]
											},
											"truncated": {
												"type": "boolean"
											},
											"export_pricing": {
												"anyOf": [
													{
														"type": "object",
														"properties": {
															"baseline_usd": {
																"type": "number"
															},
															"free_rows": {
																"type": "number"
															},
															"max_rows_ceiling": {
																"type": "number"
															},
															"row_cpm_usd": {
																"type": "number"
															}
														},
														"required": [
															"baseline_usd",
															"free_rows",
															"max_rows_ceiling",
															"row_cpm_usd"
														]
													},
													{
														"type": "null"
													}
												]
											},
											"manifest": {
												"anyOf": [
													{
														"type": "array",
														"items": {
															"type": "object",
															"properties": {
																"dtype": {
																	"anyOf": [
																		{
																			"type": "string",
																			"enum": [
																				"boolean",
																				"date",
																				"datetime",
																				"number",
																				"string"
																			]
																		},
																		{
																			"type": "null"
																		}
																	]
																},
																"entity": {
																	"anyOf": [
																		{
																			"type": "string"
																		},
																		{
																			"type": "null"
																		}
																	]
																},
																"metric": {
																	"anyOf": [
																		{
																			"type": "string"
																		},
																		{
																			"type": "null"
																		}
																	]
																},
																"name": {
																	"anyOf": [
																		{
																			"type": "string"
																		},
																		{
																			"type": "null"
																		}
																	]
																},
																"unit": {
																	"anyOf": [
																		{
																			"type": "string"
																		},
																		{
																			"type": "null"
																		}
																	]
																}
															}
														}
													},
													{
														"type": "null"
													}
												]
											}
										},
										"additionalProperties": {}
									},
									{
										"type": "null"
									}
								]
							},
							"exportable": {
								"type": "boolean"
							},
							"nodes": {
								"anyOf": [
									{
										"type": "array",
										"items": {
											"type": "object",
											"properties": {
												"id": {
													"type": "string"
												},
												"type": {
													"type": "string",
													"enum": [
														"entity",
														"metric"
													]
												},
												"name": {
													"type": "string"
												},
												"description": {
													"anyOf": [
														{
															"type": "string"
														},
														{
															"type": "null"
														}
													]
												}
											},
											"required": [
												"id",
												"type",
												"name"
											]
										}
									},
									{
										"type": "null"
									}
								]
							},
							"metric_definitions": {
								"anyOf": [
									{
										"type": "array",
										"items": {
											"type": "object",
											"properties": {
												"name": {
													"type": "string"
												},
												"definition": {
													"type": "string"
												}
											},
											"required": [
												"name",
												"definition"
											]
										}
									},
									{
										"type": "null"
									}
								]
							},
							"data_freshness": {
								"anyOf": [
									{
										"type": "object",
										"properties": {
											"coverage_end": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											},
											"data_as_of": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											},
											"last_updated": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											}
										}
									},
									{
										"type": "null"
									}
								]
							}
						},
						"additionalProperties": {}
					}
				},
				"web_results": {
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
							"snippet": {
								"anyOf": [
									{
										"type": "string"
									},
									{
										"type": "null"
									}
								]
							},
							"source_name": {
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
							"content": {
								"anyOf": [
									{
										"type": "object",
										"properties": {
											"content_format": {
												"anyOf": [
													{
														"type": "string",
														"enum": [
															"card_json",
															"csv",
															"json_compact",
															"json_records"
														]
													},
													{
														"type": "null"
													}
												]
											},
											"cost": {
												"type": "number"
											},
											"data": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											},
											"records": {
												"anyOf": [
													{
														"type": "array",
														"items": {
															"type": "object",
															"propertyNames": {
																"type": "string"
															},
															"additionalProperties": {
																"anyOf": [
																	{
																		"anyOf": [
																			{
																				"type": "boolean"
																			},
																			{
																				"type": "number"
																			},
																			{
																				"type": "string"
																			}
																		]
																	},
																	{
																		"type": "null"
																	}
																]
															}
														}
													},
													{
														"type": "null"
													}
												]
											},
											"dataset": {
												"anyOf": [
													{
														"type": "object",
														"properties": {
															"columns": {
																"type": "array",
																"items": {
																	"type": "object",
																	"properties": {
																		"name": {
																			"type": "string"
																		},
																		"type": {
																			"type": "string",
																			"enum": [
																				"boolean",
																				"date",
																				"datetime",
																				"number",
																				"string"
																			]
																		},
																		"unit": {
																			"anyOf": [
																				{
																					"type": "string"
																				},
																				{
																					"type": "null"
																				}
																			]
																		}
																	},
																	"required": [
																		"name",
																		"type"
																	]
																}
															},
															"rows": {
																"type": "array",
																"items": {
																	"type": "array",
																	"items": {
																		"anyOf": [
																			{
																				"anyOf": [
																					{
																						"type": "boolean"
																					},
																					{
																						"type": "number"
																					},
																					{
																						"type": "string"
																					}
																				]
																			},
																			{
																				"type": "null"
																			}
																		]
																	}
																}
															},
															"total_rows": {
																"type": "number"
															},
															"truncated": {
																"type": "boolean"
															},
															"ref": {
																"type": "string"
															},
															"sources": {
																"type": "array",
																"items": {
																	"type": "object",
																	"properties": {
																		"name": {
																			"type": "string"
																		},
																		"index": {
																			"type": "string",
																			"enum": [
																				"data",
																				"web"
																			]
																		}
																	},
																	"required": [
																		"name"
																	]
																}
															},
															"provenance": {
																"type": "string",
																"enum": [
																	"query",
																	"web_extraction"
																]
															}
														},
														"required": [
															"columns",
															"rows",
															"total_rows",
															"truncated",
															"ref",
															"sources"
														]
													},
													{
														"type": "null"
													}
												]
											},
											"card_data": {
												"anyOf": [
													{
														"type": "object",
														"properties": {},
														"additionalProperties": {}
													},
													{
														"type": "null"
													}
												]
											},
											"card_data_schema": {
												"anyOf": [
													{
														"type": "object",
														"properties": {},
														"additionalProperties": {}
													},
													{
														"type": "null"
													}
												]
											},
											"url": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											},
											"expires_at": {
												"anyOf": [
													{
														"type": "string"
													},
													{
														"type": "null"
													}
												]
											},
											"total_rows": {
												"anyOf": [
													{
														"type": "number"
													},
													{
														"type": "null"
													}
												]
											},
											"truncated": {
												"type": "boolean"
											},
											"export_pricing": {
												"anyOf": [
													{
														"type": "object",
														"properties": {
															"baseline_usd": {
																"type": "number"
															},
															"free_rows": {
																"type": "number"
															},
															"max_rows_ceiling": {
																"type": "number"
															},
															"row_cpm_usd": {
																"type": "number"
															}
														},
														"required": [
															"baseline_usd",
															"free_rows",
															"max_rows_ceiling",
															"row_cpm_usd"
														]
													},
													{
														"type": "null"
													}
												]
											},
											"manifest": {
												"anyOf": [
													{
														"type": "array",
														"items": {
															"type": "object",
															"properties": {
																"dtype": {
																	"anyOf": [
																		{
																			"type": "string",
																			"enum": [
																				"boolean",
																				"date",
																				"datetime",
																				"number",
																				"string"
																			]
																		},
																		{
																			"type": "null"
																		}
																	]
																},
																"entity": {
																	"anyOf": [
																		{
																			"type": "string"
																		},
																		{
																			"type": "null"
																		}
																	]
																},
																"metric": {
																	"anyOf": [
																		{
																			"type": "string"
																		},
																		{
																			"type": "null"
																		}
																	]
																},
																"name": {
																	"anyOf": [
																		{
																			"type": "string"
																		},
																		{
																			"type": "null"
																		}
																	]
																},
																"unit": {
																	"anyOf": [
																		{
																			"type": "string"
																		},
																		{
																			"type": "null"
																		}
																	]
																}
															}
														}
													},
													{
														"type": "null"
													}
												]
											}
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
							"title",
							"url"
						],
						"additionalProperties": {}
					}
				},
				"usage": {
					"anyOf": [
						{
							"type": "object",
							"properties": {
								"total_cost_usd": {
									"type": "number"
								},
								"compute": {
									"anyOf": [
										{
											"type": "object",
											"properties": {
												"cost_usd": {
													"type": "number"
												}
											},
											"required": [
												"cost_usd"
											]
										},
										{
											"type": "null"
										}
									]
								},
								"data": {
									"anyOf": [
										{
											"type": "object",
											"properties": {
												"cost_usd": {
													"type": "number"
												},
												"datasets": {
													"type": "number"
												}
											},
											"required": [
												"cost_usd",
												"datasets"
											]
										},
										{
											"type": "null"
										}
									]
								}
							},
							"required": [
								"total_cost_usd"
							]
						},
						{
							"type": "null"
						}
					]
				},
				"related": {
					"anyOf": [
						{
							"type": "array",
							"items": {
								"type": "object",
								"properties": {},
								"additionalProperties": {}
							}
						},
						{
							"type": "null"
						}
					]
				}
			},
			"required": [
				"request_id"
			],
			"additionalProperties": {}
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
						"invalid_input",
						"rate_limit",
						"timeout",
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

func takoSearchOutputSchema() map[string]interface{} {
	return mustDecodeToolSchema(takoSearchOutputSchemaJSON)
}

func takoSearchArgs(config TakoSearchConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.Effort != "" {
		args["effort"] = config.Effort
	}
	if config.Sources != nil {
		sources := map[string]interface{}{}
		if config.Sources.Data != nil {
			sources["data"] = takoDataSourceArgs(*config.Sources.Data)
		}
		if config.Sources.Web != nil {
			sources["web"] = takoWebSourceArgs(*config.Sources.Web)
		}
		args["sources"] = sources
	}
	if config.Location != nil {
		args["location"] = map[string]interface{}{
			"latitude":  config.Location.Latitude,
			"longitude": config.Location.Longitude,
		}
	}
	if config.CountryCode != "" {
		args["countryCode"] = config.CountryCode
	}
	if config.Locale != "" {
		args["locale"] = config.Locale
	}
	if config.Timezone != "" {
		args["timezone"] = config.Timezone
	}
	if config.OutputSettings != nil {
		outputSettings := map[string]interface{}{}
		if config.OutputSettings.ImageDarkMode != nil {
			outputSettings["imageDarkMode"] = *config.OutputSettings.ImageDarkMode
		}
		if config.OutputSettings.ForceRefresh != nil {
			outputSettings["forceRefresh"] = *config.OutputSettings.ForceRefresh
		}
		args["outputSettings"] = outputSettings
	}
	if config.IncludeRelated != nil {
		args["includeRelated"] = *config.IncludeRelated
	}
	return args
}

func takoDataSourceArgs(config TakoDataSourceConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.Count != nil {
		args["count"] = *config.Count
	}
	if config.IncludeContents != nil {
		args["includeContents"] = *config.IncludeContents
	}
	if config.Mode != "" {
		args["mode"] = config.Mode
	}
	if config.ContentFormat != "" {
		args["contentFormat"] = config.ContentFormat
	}
	if config.MaxRows != nil {
		args["maxRows"] = *config.MaxRows
	}
	if len(config.NodeIDs) > 0 {
		args["nodeIds"] = config.NodeIDs
	}
	if config.Strict != nil {
		args["strict"] = *config.Strict
	}
	return args
}

func takoWebSourceArgs(config TakoWebSourceConfig) map[string]interface{} {
	args := map[string]interface{}{}
	if config.Count != nil {
		args["count"] = *config.Count
	}
	if config.IncludeContents != nil {
		args["includeContents"] = *config.IncludeContents
	}
	if config.Category != "" {
		args["category"] = config.Category
	}
	if len(config.IncludeDomains) > 0 {
		args["includeDomains"] = config.IncludeDomains
	}
	if len(config.ExcludeDomains) > 0 {
		args["excludeDomains"] = config.ExcludeDomains
	}
	if config.SnippetMaxChars != nil {
		args["snippetMaxChars"] = *config.SnippetMaxChars
	}
	if config.Highlights != nil {
		args["highlights"] = *config.Highlights
	}
	if config.ArticleContentMaxChars != nil {
		args["articleContentMaxChars"] = *config.ArticleContentMaxChars
	}
	if config.PublishedAfter != "" {
		args["publishedAfter"] = config.PublishedAfter
	}
	if config.PublishedBefore != "" {
		args["publishedBefore"] = config.PublishedBefore
	}
	return args
}

func (t TakoSearchTool) ToTool() types.Tool {
	return types.Tool(t)
}
