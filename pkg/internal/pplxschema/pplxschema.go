// Package pplxschema ports the Perplexity Agent API zod schemas
// (perplexity/src/perplexity-agent-api.ts): perplexityAgentChunkSchema for
// stream frames and perplexityAgentResponseSchema for complete responses.
// It is internal so the schemas stay out of the public API.
package pplxschema

import "github.com/digitallysavvy/go-ai/pkg/internal/zodlite"

type zNode = zodlite.Node

var (
	zStr      = zodlite.String
	zNum      = zodlite.Number
	zNullish  = zodlite.Nullish
	zOptional = zodlite.Optional
	zArr      = zodlite.Array
	zRec      = zodlite.RecordOf
	fl        = zodlite.F
	zObj      = zodlite.Object
	nStr      = zNullish(zStr)
	nNum      = zNullish(zNum)
)

func zLit(want string) zNode { return zodlite.Enum(want) }

// Response is perplexityAgentResponseSchema and Chunk is
// perplexityAgentChunkSchema.
var (
	zPplxSearchResult = zObj(fl("id", zOptional(zNum)), fl("title", zStr), fl("url", zStr), fl("snippet", zOptional(zStr)),
		fl("date", nStr), fl("last_updated", nStr), fl("source", zOptional(zStr)))
	zPplxFetched = zObj(fl("title", zStr), fl("url", zStr), fl("snippet", zOptional(zStr)))

	zPplxContentPart = zObj(fl("type", zStr), fl("text", zOptional(zStr)),
		fl("annotations", zNullish(zArr(zObj(fl("type", zOptional(zStr)), fl("url", zOptional(zStr)), fl("title", zOptional(zStr)))))))

	zPplxHandledItems = map[string]zNode{
		"message":           zObj(fl("type", zLit("message")), fl("id", nStr), fl("content", zNullish(zArr(zPplxContentPart)))),
		"search_results":    zObj(fl("type", zLit("search_results")), fl("results", zNullish(zArr(zPplxSearchResult)))),
		"fetch_url_results": zObj(fl("type", zLit("fetch_url_results")), fl("contents", zNullish(zArr(zPplxFetched)))),
		"function_call": zObj(fl("type", zLit("function_call")), fl("id", nStr), fl("call_id", nStr), fl("name", nStr),
			fl("arguments", nStr), fl("thought_signature", nStr)),
	}

	// zPplxItem is perplexityOutputItemSchema: the four handled shapes, or any
	// object with a string `type` that is not one of them.
	zPplxItem zNode = func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return &zodlite.Issue{Path: path, Code: "invalid_union", Message: "Invalid input"}
		}
		t, isStr := m["type"].(string)
		if !isStr {
			return &zodlite.Issue{Path: path, Code: "invalid_union", Message: "Invalid input"}
		}
		if n, handled := zPplxHandledItems[t]; handled {
			return n(v, present, path)
		}
		return nil
	}

	zPplxCost = zObj(fl("currency", zOptional(zStr)), fl("input_cost", zOptional(zNum)), fl("output_cost", zOptional(zNum)),
		fl("total_cost", zOptional(zNum)), fl("cache_creation_cost", nNum), fl("cache_read_cost", nNum), fl("tool_calls_cost", nNum))

	zPplxUsage = zObj(
		fl("input_tokens", zNum), fl("output_tokens", zNum), fl("total_tokens", zNum),
		fl("input_tokens_details", zNullish(zObj(fl("cached_tokens", zOptional(zNum)), fl("cache_creation_input_tokens", zOptional(zNum)),
			fl("cache_read_input_tokens", zOptional(zNum))))),
		fl("output_tokens_details", zNullish(zObj(fl("reasoning_tokens", zOptional(zNum))))),
		fl("tool_calls_details", zNullish(zRec(zObj(fl("invocation", zOptional(zNum)))))),
		fl("cost", zNullish(zPplxCost)),
	)

	Response = zObj(
		fl("id", zStr), fl("created_at", zNum), fl("model", zStr), fl("object", zLit("response")),
		fl("output", zArr(zPplxItem)), fl("status", zStr),
		fl("incomplete_details", zNullish(zObj(fl("reason", zStr)))),
		fl("error", zNullish(zObj(fl("code", zOptional(zStr)), fl("message", zStr), fl("type", zOptional(zStr))))),
		fl("usage", zNullish(zPplxUsage)),
	)

	Chunk = zObj(
		fl("type", zStr), fl("sequence_number", nNum), fl("response", zNullish(Response)), fl("item", zNullish(zPplxItem)),
		fl("output_index", nNum), fl("item_id", nStr), fl("content_index", nNum), fl("delta", nStr), fl("text", nStr), fl("thought", nStr),
		fl("queries", zNullish(zArr(zStr))), fl("urls", zNullish(zArr(zStr))),
		fl("results", zNullish(zArr(zPplxSearchResult))), fl("contents", zNullish(zArr(zPplxFetched))),
		fl("error", zNullish(zObj(fl("code", nStr), fl("message", zStr), fl("type", nStr)))),
	)
)
