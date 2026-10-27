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

	// zPplxUnhandled is the last union member: an object whose string `type`
	// is refined to not be one of the handled types (custom issue at `type`).
	zPplxUnhandled = zObj(fl("type", func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		if bad := zStr(v, present, path); bad != nil {
			return bad
		}
		if _, handled := zPplxHandledItems[v.(string)]; handled {
			return &zodlite.Issue{Path: path, Code: "custom", Message: "Invalid input"}
		}
		return nil
	}))

	// zPplxItem is perplexityOutputItemSchema: z.union of the four handled
	// shapes and the unhandled catch-all (a malformed handled item fails every
	// member, so it is an invalid_union issue like TS).
	zPplxItem = zodlite.Union(
		zPplxHandledItems["message"], zPplxHandledItems["search_results"],
		zPplxHandledItems["fetch_url_results"], zPplxHandledItems["function_call"], zPplxUnhandled)

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

// Error is perplexityErrorSchema: `error`, `detail` and `message` are
// optional (an explicit null fails).
var Error = zObj(
	fl("error", zOptional(zodlite.Union(zStr, zObj(
		fl("code", zNullish(zodlite.Union(zStr, zNum))), fl("message", nStr), fl("type", nStr))))),
	fl("detail", zOptional(zodlite.Union(zStr, zArr(zObj(fl("msg", zStr)))))),
	fl("message", zOptional(zStr)),
)
