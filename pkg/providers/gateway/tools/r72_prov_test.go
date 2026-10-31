package tools

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r72provStrings reads a JSON schema string list ("required", "enum") that
// is either a []string literal or a decoded []interface{}.
func r72provStrings(v interface{}) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []interface{}:
		out := make([]string, 0, len(s))
		for _, e := range s {
			str, _ := e.(string)
			out = append(out, str)
		}
		return out
	}
	return nil
}

// r72provTSSchemas holds the input and output schemas of every TS gateway
// tool (packages/gateway/src/tool/*.ts at e64ed82f27), produced by zod 4's
// toJSONSchema(schema, {target: 'draft-7', io: 'input', reused: 'inline'})
// as zodSchema calls it, minus "$schema". The Go schemas must equal them:
// provider tool calls are validated against Parameters and
// ValidateUIMessages checks tool outputs against OutputSchema.
const r72provTSSchemas = `
{"gateway.browserbase_fetch":{"input":{"type":"object","properties":{"url":{"type":"string","format":"uri","description":"URL of the page to fetch."},"allow_redirects":{"description":"Whether to follow HTTP redirects (default: false).","type":"boolean"},"allow_insecure_ssl":{"description":"Whether to bypass TLS certificate verification (default: false). Only use for trusted hosts.","type":"boolean"},"proxies":{"description":"Whether to route the request through Browserbase proxies (default: false).","type":"boolean"},"format":{"description":"Output format. raw returns the response body unchanged, markdown returns page content as Markdown, and json returns structured content using schema.","type":"string","enum":["raw","json","markdown"]},"schema":{"description":"JSON Schema for structured extraction. Only use with format set to json.","type":"object","propertyNames":{"type":"string"},"additionalProperties":{}}},"required":["url"]},"output":{"anyOf":[{"type":"object","properties":{"id":{"type":"string"},"content":{"anyOf":[{"type":"string"},{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{}}]},"contentType":{"type":"string"},"encoding":{"type":"string"},"headers":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"string"}},"statusCode":{"type":"number"}},"required":["id","content","contentType","encoding","headers","statusCode"]},{"type":"object","properties":{"error":{"type":"string","enum":["api_error","configuration_error","execution_error","invalid_input","rate_limit","timeout","unknown"]},"statusCode":{"type":"number"},"message":{"type":"string"}},"required":["error","message"]}]}},"gateway.browserbase_search":{"input":{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":200,"description":"Web search query. Must be between 1 and 200 characters."},"num_results":{"description":"Maximum number of results to return (1-25, default: 10).","type":"integer","minimum":1,"maximum":25}},"required":["query"]},"output":{"anyOf":[{"type":"object","properties":{"query":{"type":"string"},"requestId":{"type":"string"},"results":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"},"url":{"type":"string"},"author":{"type":"string"},"favicon":{"type":"string"},"image":{"type":"string"},"publishedDate":{"type":"string"}},"required":["id","title","url"]}}},"required":["query","requestId","results"]},{"type":"object","properties":{"error":{"type":"string","enum":["api_error","configuration_error","execution_error","invalid_input","rate_limit","timeout","unknown"]},"statusCode":{"type":"number"},"message":{"type":"string"}},"required":["error","message"]}]}},"gateway.exa_search":{"input":{"type":"object","properties":{"query":{"type":"string","description":"Natural-language web search query. This is required."},"type":{"description":"Search method. Use auto for the default balance of speed and quality.","type":"string","enum":["auto","fast","instant"]},"num_results":{"description":"Maximum number of results to return (1-100, default: 10).","type":"number"},"category":{"description":"Optional content category to focus results.","type":"string","enum":["company","people","research paper","news","personal site","financial report"]},"user_location":{"description":"Two-letter ISO country code such as 'US'.","type":"string"},"include_domains":{"description":"Only return results from these domains.","type":"array","items":{"type":"string"}},"exclude_domains":{"description":"Exclude results from these domains.","type":"array","items":{"type":"string"}},"start_published_date":{"description":"Only return links published after this ISO 8601 date.","type":"string"},"end_published_date":{"description":"Only return links published before this ISO 8601 date.","type":"string"},"contents":{"description":"Controls extracted page content and freshness.","type":"object","properties":{"text":{"anyOf":[{"type":"boolean"},{"type":"object","properties":{"max_characters":{"type":"number"},"include_html_tags":{"type":"boolean"},"verbosity":{"type":"string","enum":["compact","standard","full"]},"include_sections":{"type":"array","items":{"type":"string","enum":["header","navigation","banner","body","sidebar","footer","metadata"]}},"exclude_sections":{"type":"array","items":{"type":"string","enum":["header","navigation","banner","body","sidebar","footer","metadata"]}}}}]},"highlights":{"anyOf":[{"type":"boolean"},{"type":"object","properties":{"query":{"type":"string"},"max_characters":{"type":"number"}}}]},"max_age_hours":{"type":"number"},"livecrawl_timeout":{"type":"number"},"subpages":{"type":"number"},"subpage_target":{"anyOf":[{"type":"string"},{"type":"array","items":{"type":"string"}}]},"extras":{"type":"object","properties":{"links":{"type":"number"},"image_links":{"type":"number"}}}}}},"required":["query"]},"output":{"anyOf":[{"type":"object","properties":{"requestId":{"type":"string"},"searchType":{"type":"string"},"resolvedSearchType":{"type":"string"},"results":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"url":{"type":"string"},"id":{"type":"string"},"publishedDate":{"anyOf":[{"type":"string"},{"type":"null"}]},"author":{"anyOf":[{"type":"string"},{"type":"null"}]},"image":{"anyOf":[{"type":"string"},{"type":"null"}]},"favicon":{"anyOf":[{"type":"string"},{"type":"null"}]},"text":{"type":"string"},"highlights":{"type":"array","items":{"type":"string"}},"highlightScores":{"type":"array","items":{"type":"number"}},"summary":{"type":"string"},"subpages":{"type":"array","items":{}},"extras":{"type":"object","properties":{"links":{"type":"array","items":{"type":"string"}},"imageLinks":{"type":"array","items":{"type":"string"}}}}},"required":["title","url","id"]}},"costDollars":{"type":"object","properties":{"total":{"type":"number"},"search":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"number"}}}}},"required":["requestId","results"]},{"type":"object","properties":{"error":{"type":"string","enum":["api_error","rate_limit","timeout","invalid_input","configuration_error","execution_error","unknown"]},"statusCode":{"type":"number"},"message":{"type":"string"}},"required":["error","message"]}]}},"gateway.parallel_search":{"input":{"type":"object","properties":{"objective":{"type":"string","description":"Natural-language description of the web research goal, including source or freshness guidance and broader context from the task. Maximum 5000 characters."},"search_queries":{"description":"Provide 2-3 concise keyword queries, 3-6 words each (max 5, 200 chars each). Strongly recommended; the search runs on these queries.","type":"array","items":{"type":"string"}},"mode":{"description":"Search mode: \"turbo\" and \"fast\" return quickly with shorter excerpts, \"basic\" is balanced (default), \"advanced\" is the most thorough. \"turbo\" supports English and Japanese only. \"one-shot\" and \"agentic\" are deprecated aliases of \"basic\" and \"advanced\".","type":"string","enum":["turbo","fast","basic","advanced","one-shot","agentic"]},"max_results":{"description":"Maximum number of results to return (1-20). Defaults to 10 if not specified.","type":"number"},"source_policy":{"description":"Source policy for controlling which domains to include/exclude and freshness.","type":"object","properties":{"include_domains":{"description":"Domains or domain/path prefixes to restrict results to, without scheme or port. Example: ['wikipedia.org', 'docs.python.org/3'].","type":"array","items":{"type":"string"}},"exclude_domains":{"description":"Domains or domain/path prefixes to exclude, without scheme or port. Example: ['reddit.com'].","type":"array","items":{"type":"string"}},"after_date":{"description":"Only include results published on or after this date. Use an ISO 8601 calendar date formatted YYYY-MM-DD (e.g. 2025-01-01); do not include a time.","type":"string"}}},"excerpts":{"description":"Excerpt configuration for controlling result length.","type":"object","properties":{"max_chars_per_result":{"description":"Maximum characters per result.","type":"number"},"max_chars_total":{"description":"Deprecated: use the top-level max_chars_total.","type":"number"}}},"max_chars_total":{"description":"Maximum total characters across all result excerpts.","type":"number"},"fetch_policy":{"description":"Fetch policy for controlling content freshness.","type":"object","properties":{"max_age_seconds":{"description":"Maximum age of cached content in seconds before a live fetch, minimum 600.","type":"number"},"timeout_seconds":{"description":"Timeout in seconds for a live fetch when content is not cached. Capped by the gateway tool timeout.","type":"number"},"disable_cache_fallback":{"description":"Return an error instead of older cached content when a live fetch fails.","type":"boolean"}}},"location":{"description":"ISO 3166-1 alpha-2 country code to localize results, e.g. \"us\".","type":"string"}},"required":["objective"]},"output":{"anyOf":[{"type":"object","properties":{"search_id":{"type":"string"},"results":{"type":"array","items":{"type":"object","properties":{"url":{"type":"string"},"title":{"anyOf":[{"type":"string"},{"type":"null"}]},"publish_date":{"anyOf":[{"type":"string"},{"type":"null"}]},"excerpts":{"type":"array","items":{"type":"string"}}},"required":["url","excerpts"]}},"warnings":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"type":{"type":"string"},"message":{"type":"string"},"detail":{"anyOf":[{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{}},{"type":"null"}]}},"required":["type","message"]}},{"type":"null"}]},"usage":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"count":{"type":"number"}},"required":["name"]}},{"type":"null"}]},"session_id":{"type":"string"}},"required":["search_id","results"]},{"type":"object","properties":{"error":{"type":"string","enum":["api_error","configuration_error","execution_error","gateway_rate_limit","invalid_input","rate_limit","timeout","unknown","unknown_tool"]},"statusCode":{"type":"number"},"message":{"type":"string"}},"required":["error","message"]}]}},"gateway.perplexity_search":{"input":{"type":"object","properties":{"query":{"anyOf":[{"type":"string"},{"type":"array","items":{"type":"string"}}],"description":"Search query (string) or multiple queries (array of up to 5 strings). Multi-query searches return combined results from all queries."},"max_results":{"description":"Maximum number of search results to return (1-20, default: 10)","type":"number"},"max_tokens_per_page":{"description":"Maximum number of tokens to extract per search result page (256-2048, default: 2048)","type":"number"},"max_tokens":{"description":"Maximum total tokens across all search results (default: 25000, max: 1000000)","type":"number"},"country":{"description":"Two-letter ISO 3166-1 alpha-2 country code for regional search results (e.g., 'US', 'GB', 'FR')","type":"string"},"search_domain_filter":{"description":"List of domains to include or exclude from search results (max 20). To include: ['nature.com', 'science.org']. To exclude: ['-example.com', '-spam.net']","type":"array","items":{"type":"string"}},"search_language_filter":{"description":"List of ISO 639-1 language codes to filter results (max 10, lowercase). Examples: ['en', 'fr', 'de']","type":"array","items":{"type":"string"}},"search_after_date":{"description":"Include only results published after this date. Format: 'MM/DD/YYYY' (e.g., '3/1/2025'). Cannot be used with search_recency_filter.","type":"string"},"search_before_date":{"description":"Include only results published before this date. Format: 'MM/DD/YYYY' (e.g., '3/15/2025'). Cannot be used with search_recency_filter.","type":"string"},"last_updated_after_filter":{"description":"Include only results last updated after this date. Format: 'MM/DD/YYYY' (e.g., '3/1/2025'). Cannot be used with search_recency_filter.","type":"string"},"last_updated_before_filter":{"description":"Include only results last updated before this date. Format: 'MM/DD/YYYY' (e.g., '3/15/2025'). Cannot be used with search_recency_filter.","type":"string"},"search_recency_filter":{"description":"Filter results by relative time period. Cannot be used with search_after_date or search_before_date.","type":"string","enum":["day","week","month","year"]}},"required":["query"]},"output":{"anyOf":[{"type":"object","properties":{"results":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"url":{"type":"string"},"snippet":{"type":"string"},"date":{"type":"string"},"lastUpdated":{"type":"string"}},"required":["title","url","snippet"]}},"id":{"type":"string"}},"required":["results","id"]},{"type":"object","properties":{"error":{"type":"string","enum":["api_error","rate_limit","timeout","invalid_input","unknown"]},"statusCode":{"type":"number"},"message":{"type":"string"}},"required":["error","message"]}]}},"gateway.tako_search":{"input":{"type":"object","properties":{"query":{"type":"string","description":"Natural-language search query. Include the entity, metric, and time period. Quote a phrase to force it to one entity, for example \"Tesla\":PRODUCT price."},"effort":{"description":"Search effort. fast is the balanced default, instant favors cached results and low latency, and deep broadens retrieval with reranking at higher cost and latency.","type":"string","enum":["deep","fast","instant"]},"sources":{"description":"Sources to search. Omit to search both curated data and the web. When provided, only keys present are searched.","type":"object","properties":{"data":{"type":"object","properties":{"count":{"description":"Maximum number of data results to return (1-20). When include_contents is true, each additional result adds its own data surcharge.","type":"number"},"include_contents":{"description":"Inline rows for each data result. This adds a data surcharge based on row count and dataset source. To estimate cost, search with include_contents disabled and inspect cards.content.export_pricing. This applies to every returned card; limit sources.data.count and sources.data.max_rows to control cost.","type":"boolean"},"mode":{"description":"Requested data delivery mode. Search card data is always inline.","type":"string","enum":["inline","url"]},"content_format":{"description":"Serialization for inlined card data.","type":"string","enum":["card_json","csv","json_compact","json_records"]},"max_rows":{"description":"Maximum rows to inline per result. Omit to use the allowance in cards.content.export_pricing. A data surcharge applies per 1,000 exported rows; lower values reduce cost.","type":"number"},"node_ids":{"description":"Data Graph node IDs to prioritize. Maximum 20.","type":"array","items":{"type":"string"}},"strict":{"description":"Only return cards matching node_ids. Requires a non-empty node_ids.","type":"boolean"}}},"web":{"type":"object","properties":{"count":{"description":"Maximum number of web results to return (1-20).","type":"number"},"include_contents":{"description":"Inline extracted web page text. This can add a data charge.","type":"boolean"},"category":{"description":"Optional web-result category filter.","type":"string","enum":["finance","news","sports"]},"include_domains":{"description":"Only return results from these bare domains.","type":"array","items":{"type":"string"}},"exclude_domains":{"description":"Exclude results from these bare domains.","type":"array","items":{"type":"string"}},"snippet_max_chars":{"description":"Maximum characters in each web-result snippet.","type":"number"},"highlights":{"description":"Include highlighted passages in web results. Defaults to true in AI Gateway.","type":"boolean"},"article_content_max_chars":{"description":"Maximum extracted characters per web page when including contents.","type":"number"},"published_after":{"description":"Keep results published on or after this ISO date (YYYY-MM-DD).","type":"string"},"published_before":{"description":"Keep results published on or before this ISO date (YYYY-MM-DD).","type":"string"}}}}},"location":{"description":"End-user coordinates for localized results.","type":"object","properties":{"latitude":{"type":"number","description":"Latitude between -90 and 90."},"longitude":{"type":"number","description":"Longitude between -180 and 180."}},"required":["latitude","longitude"]},"country_code":{"description":"Two-letter ISO 3166-1 country code, such as 'US'.","type":"string"},"locale":{"description":"BCP-47 locale, such as 'en-US'.","type":"string"},"timezone":{"description":"IANA timezone, such as 'America/New_York'.","type":"string"},"output_settings":{"description":"Controls card rendering in the search response.","type":"object","properties":{"image_dark_mode":{"description":"Render card preview images in dark mode.","type":"boolean"},"force_refresh":{"description":"Instant-effort only. Request a refreshed instant result.","type":"boolean"}}},"include_related":{"description":"Maximum related search suggestions to include (1-20).","type":"number"}},"required":["query"]},"output":{"anyOf":[{"type":"object","properties":{"request_id":{"type":"string"},"cards":{"type":"array","items":{"type":"object","properties":{"card_id":{"anyOf":[{"type":"string"},{"type":"null"}]},"title":{"anyOf":[{"type":"string"},{"type":"null"}]},"description":{"anyOf":[{"type":"string"},{"type":"null"}]},"semantic_description":{"anyOf":[{"type":"string"},{"type":"null"}]},"webpage_url":{"anyOf":[{"type":"string"},{"type":"null"}]},"image_url":{"anyOf":[{"type":"string"},{"type":"null"}]},"embed_url":{"anyOf":[{"type":"string"},{"type":"null"}]},"sources":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"source_name":{"anyOf":[{"type":"string"},{"type":"null"}]},"source_description":{"anyOf":[{"type":"string"},{"type":"null"}]},"source_index":{"type":"string","enum":["data","web"]},"source_text":{"anyOf":[{"type":"string"},{"type":"null"}]},"url":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["source_index"]}},{"type":"null"}]},"methodologies":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"methodology_name":{"anyOf":[{"type":"string"},{"type":"null"}]},"methodology_description":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["methodology_name","methodology_description"]}},{"type":"null"}]},"source_indexes":{"anyOf":[{"type":"array","items":{"type":"string","enum":["data","web"]}},{"type":"null"}]},"card_type":{"anyOf":[{"type":"string"},{"type":"null"}]},"relevance":{"anyOf":[{"type":"string","enum":["High","Low","Medium"]},{"type":"null"}]},"content":{"anyOf":[{"type":"object","properties":{"content_format":{"anyOf":[{"type":"string","enum":["card_json","csv","json_compact","json_records"]},{"type":"null"}]},"cost":{"type":"number"},"data":{"anyOf":[{"type":"string"},{"type":"null"}]},"records":{"anyOf":[{"type":"array","items":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"anyOf":[{"anyOf":[{"type":"boolean"},{"type":"number"},{"type":"string"}]},{"type":"null"}]}}},{"type":"null"}]},"dataset":{"anyOf":[{"type":"object","properties":{"columns":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"type":{"type":"string","enum":["boolean","date","datetime","number","string"]},"unit":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["name","type"]}},"rows":{"type":"array","items":{"type":"array","items":{"anyOf":[{"anyOf":[{"type":"boolean"},{"type":"number"},{"type":"string"}]},{"type":"null"}]}}},"total_rows":{"type":"number"},"truncated":{"type":"boolean"},"ref":{"type":"string"},"sources":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"index":{"type":"string","enum":["data","web"]}},"required":["name"]}},"provenance":{"type":"string","enum":["query","web_extraction"]}},"required":["columns","rows","total_rows","truncated","ref","sources"]},{"type":"null"}]},"card_data":{"anyOf":[{"type":"object","properties":{},"additionalProperties":{}},{"type":"null"}]},"card_data_schema":{"anyOf":[{"type":"object","properties":{},"additionalProperties":{}},{"type":"null"}]},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"expires_at":{"anyOf":[{"type":"string"},{"type":"null"}]},"total_rows":{"anyOf":[{"type":"number"},{"type":"null"}]},"truncated":{"type":"boolean"},"export_pricing":{"anyOf":[{"type":"object","properties":{"baseline_usd":{"type":"number"},"free_rows":{"type":"number"},"max_rows_ceiling":{"type":"number"},"row_cpm_usd":{"type":"number"}},"required":["baseline_usd","free_rows","max_rows_ceiling","row_cpm_usd"]},{"type":"null"}]},"manifest":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"dtype":{"anyOf":[{"type":"string","enum":["boolean","date","datetime","number","string"]},{"type":"null"}]},"entity":{"anyOf":[{"type":"string"},{"type":"null"}]},"metric":{"anyOf":[{"type":"string"},{"type":"null"}]},"name":{"anyOf":[{"type":"string"},{"type":"null"}]},"unit":{"anyOf":[{"type":"string"},{"type":"null"}]}}}},{"type":"null"}]}},"additionalProperties":{}},{"type":"null"}]},"exportable":{"type":"boolean"},"nodes":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"type":{"type":"string","enum":["entity","metric"]},"name":{"type":"string"},"description":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["id","type","name"]}},{"type":"null"}]},"metric_definitions":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"definition":{"type":"string"}},"required":["name","definition"]}},{"type":"null"}]},"data_freshness":{"anyOf":[{"type":"object","properties":{"coverage_end":{"anyOf":[{"type":"string"},{"type":"null"}]},"data_as_of":{"anyOf":[{"type":"string"},{"type":"null"}]},"last_updated":{"anyOf":[{"type":"string"},{"type":"null"}]}}},{"type":"null"}]}},"additionalProperties":{}}},"web_results":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"url":{"type":"string"},"snippet":{"anyOf":[{"type":"string"},{"type":"null"}]},"source_name":{"anyOf":[{"type":"string"},{"type":"null"}]},"publish_date":{"anyOf":[{"type":"string"},{"type":"null"}]},"content":{"anyOf":[{"type":"object","properties":{"content_format":{"anyOf":[{"type":"string","enum":["card_json","csv","json_compact","json_records"]},{"type":"null"}]},"cost":{"type":"number"},"data":{"anyOf":[{"type":"string"},{"type":"null"}]},"records":{"anyOf":[{"type":"array","items":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"anyOf":[{"anyOf":[{"type":"boolean"},{"type":"number"},{"type":"string"}]},{"type":"null"}]}}},{"type":"null"}]},"dataset":{"anyOf":[{"type":"object","properties":{"columns":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"type":{"type":"string","enum":["boolean","date","datetime","number","string"]},"unit":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["name","type"]}},"rows":{"type":"array","items":{"type":"array","items":{"anyOf":[{"anyOf":[{"type":"boolean"},{"type":"number"},{"type":"string"}]},{"type":"null"}]}}},"total_rows":{"type":"number"},"truncated":{"type":"boolean"},"ref":{"type":"string"},"sources":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"index":{"type":"string","enum":["data","web"]}},"required":["name"]}},"provenance":{"type":"string","enum":["query","web_extraction"]}},"required":["columns","rows","total_rows","truncated","ref","sources"]},{"type":"null"}]},"card_data":{"anyOf":[{"type":"object","properties":{},"additionalProperties":{}},{"type":"null"}]},"card_data_schema":{"anyOf":[{"type":"object","properties":{},"additionalProperties":{}},{"type":"null"}]},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"expires_at":{"anyOf":[{"type":"string"},{"type":"null"}]},"total_rows":{"anyOf":[{"type":"number"},{"type":"null"}]},"truncated":{"type":"boolean"},"export_pricing":{"anyOf":[{"type":"object","properties":{"baseline_usd":{"type":"number"},"free_rows":{"type":"number"},"max_rows_ceiling":{"type":"number"},"row_cpm_usd":{"type":"number"}},"required":["baseline_usd","free_rows","max_rows_ceiling","row_cpm_usd"]},{"type":"null"}]},"manifest":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"dtype":{"anyOf":[{"type":"string","enum":["boolean","date","datetime","number","string"]},{"type":"null"}]},"entity":{"anyOf":[{"type":"string"},{"type":"null"}]},"metric":{"anyOf":[{"type":"string"},{"type":"null"}]},"name":{"anyOf":[{"type":"string"},{"type":"null"}]},"unit":{"anyOf":[{"type":"string"},{"type":"null"}]}}}},{"type":"null"}]}},"additionalProperties":{}},{"type":"null"}]}},"required":["title","url"],"additionalProperties":{}}},"usage":{"anyOf":[{"type":"object","properties":{"total_cost_usd":{"type":"number"},"compute":{"anyOf":[{"type":"object","properties":{"cost_usd":{"type":"number"}},"required":["cost_usd"]},{"type":"null"}]},"data":{"anyOf":[{"type":"object","properties":{"cost_usd":{"type":"number"},"datasets":{"type":"number"}},"required":["cost_usd","datasets"]},{"type":"null"}]}},"required":["total_cost_usd"]},{"type":"null"}]},"related":{"anyOf":[{"type":"array","items":{"type":"object","properties":{},"additionalProperties":{}}},{"type":"null"}]}},"required":["request_id"],"additionalProperties":{}},{"type":"object","properties":{"error":{"type":"string","enum":["api_error","configuration_error","execution_error","invalid_input","rate_limit","timeout","unknown_tool"]},"statusCode":{"type":"number"},"message":{"type":"string"}},"required":["error","message"]}]}}}`

// r72provNormalize round-trips v through JSON and sorts "required" lists so
// that Go []string literals and TS output compare equal.
func r72provNormalize(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	var walk func(interface{})
	walk = func(n interface{}) {
		switch x := n.(type) {
		case map[string]interface{}:
			for k, child := range x {
				if k == "required" {
					if list, ok := child.([]interface{}); ok {
						sort.Slice(list, func(i, j int) bool { return list[i].(string) < list[j].(string) })
					}
				}
				walk(child)
			}
		case []interface{}:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(out)
	return out
}

func r72provAllTools() []types.Tool {
	return []types.Tool{
		NewBrowserbaseFetch(BrowserbaseFetchConfig{}).ToTool(),
		NewBrowserbaseSearch(BrowserbaseSearchConfig{}).ToTool(),
		NewExaSearch(ExaSearchConfig{}).ToTool(),
		NewParallelSearch(ParallelSearchConfig{}).ToTool(),
		NewPerplexitySearch(PerplexitySearchConfig{}).ToTool(),
		NewTakoSearch(TakoSearchConfig{}).ToTool(),
	}
}

// Every gateway tool's Parameters and OutputSchema equal the TS zod schemas
// (R72_CARRYIN item 3: browserbase search lacked query min/max length and
// num_results integer/min/max; the output schemas used "nullable", which the
// Go validator ignores, so null fields failed ValidateUIMessages, and lacked
// TS's required lists).
func TestR72ProvGatewayToolSchemasEqualTS(t *testing.T) {
	var ts map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(r72provTSSchemas), &ts); err != nil {
		t.Fatal(err)
	}
	tools := r72provAllTools()
	if len(tools) != len(ts) {
		t.Fatalf("Go has %d gateway tools, TS has %d", len(tools), len(ts))
	}
	for _, tool := range tools {
		want, ok := ts[tool.ProviderID]
		if !ok {
			t.Errorf("%s: no TS tool", tool.ProviderID)
			continue
		}
		if got, w := r72provNormalize(t, tool.Parameters), r72provNormalize(t, want["input"]); !reflect.DeepEqual(got, w) {
			gb, _ := json.Marshal(got)
			wb, _ := json.Marshal(w)
			t.Errorf("%s Parameters:\n got %s\nwant %s", tool.ProviderID, gb, wb)
		}
		if got, w := r72provNormalize(t, tool.OutputSchema), r72provNormalize(t, want["output"]); !reflect.DeepEqual(got, w) {
			gb, _ := json.Marshal(got)
			wb, _ := json.Marshal(w)
			t.Errorf("%s OutputSchema:\n got %s\nwant %s", tool.ProviderID, gb, wb)
		}
	}
}

func r72provDecode(t *testing.T, s string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// The browserbase input bounds reject what TS zod rejects (the provider tool
// call becomes invalid) and accept the boundary values.
func TestR72ProvBrowserbaseInputBounds(t *testing.T) {
	search := schema.NewJSONSchema(NewBrowserbaseSearch(BrowserbaseSearchConfig{}).ToTool().Parameters.(map[string]interface{}))
	fetch := schema.NewJSONSchema(NewBrowserbaseFetch(BrowserbaseFetchConfig{}).ToTool().Parameters.(map[string]interface{}))
	cases := []struct {
		name  string
		v     *schema.JSONSchemaValidator
		input string
		valid bool
	}{
		{"query 1 char", search, `{"query":"g"}`, true},
		{"query 200 chars", search, `{"query":"` + strings.Repeat("q", 200) + `"}`, true},
		{"num_results 1", search, `{"query":"go","num_results":1}`, true},
		{"num_results 25", search, `{"query":"go","num_results":25}`, true},
		{"empty query", search, `{"query":""}`, false},
		{"query 201 chars", search, `{"query":"` + strings.Repeat("q", 201) + `"}`, false},
		{"num_results 0", search, `{"query":"go","num_results":0}`, false},
		{"num_results 26", search, `{"query":"go","num_results":26}`, false},
		{"num_results 2.5", search, `{"query":"go","num_results":2.5}`, false},
		{"fetch url", fetch, `{"url":"https://example.com/page"}`, true},
		{"fetch not a url", fetch, `{"url":"not a url"}`, false},
	}
	for _, c := range cases {
		err := c.v.Validate(r72provDecode(t, c.input))
		if c.valid && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if !c.valid && err == nil {
			t.Errorf("%s: accepted, want invalid (TS zod rejects it)", c.name)
		}
	}
}

// The output schemas accept the null fields TS's .nullish()/.nullable()
// accept and enforce TS's required fields (ValidateUIMessages).
func TestR72ProvGatewayOutputSchemasNullableAndRequired(t *testing.T) {
	validator := func(tool types.Tool) schema.Validator {
		return schema.NewSimpleJSONSchema(tool.OutputSchema.(map[string]interface{})).Validator()
	}
	exa := validator(NewExaSearch(ExaSearchConfig{}).ToTool())
	tako := validator(NewTakoSearch(TakoSearchConfig{}).ToTool())
	parallel := validator(NewParallelSearch(ParallelSearchConfig{}).ToTool())
	bb := validator(NewBrowserbaseSearch(BrowserbaseSearchConfig{}).ToTool())
	cases := []struct {
		name  string
		v     schema.Validator
		value string
		valid bool
	}{
		{"exa null author", exa, `{"requestId":"r","results":[{"title":"t","url":"u","id":"i","author":null,"publishedDate":null}]}`, true},
		{"exa result without url", exa, `{"requestId":"r","results":[{"title":"t","id":"i"}]}`, false},
		{"exa error", exa, `{"error":"timeout","message":"m"}`, true},
		{"tako null card fields", tako, `{"request_id":"r","cards":[{"card_id":null,"title":null,"content":null}],"web_results":[{"title":"t","url":"u","snippet":null}],"usage":null}`, true},
		{"parallel null title", parallel, `{"search_id":"s","results":[{"url":"u","title":null,"excerpts":[]}]}`, true},
		{"browserbase result without id", bb, `{"query":"q","requestId":"r","results":[{"title":"t","url":"u"}]}`, false},
	}
	for _, c := range cases {
		err := c.v.Validate(r72provDecode(t, c.value))
		if c.valid && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if !c.valid && err == nil {
			t.Errorf("%s: accepted, want invalid (TS zod rejects it)", c.name)
		}
	}
}
