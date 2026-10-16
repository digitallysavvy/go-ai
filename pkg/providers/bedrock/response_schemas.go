package bedrock

import (
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// This file ports the zod response schemas the TS Amazon Bedrock provider
// hands to createJsonResponseHandler (AmazonBedrockResponseSchema,
// AmazonBedrockEmbeddingResponseSchema, amazonBedrockImageResponseSchema,
// amazonBedrockRerankingResponseSchema). A 2xx body that does not match is an
// APICallError "Invalid JSON response" (status, headers and body kept) caused
// by the TypeValidationError. Objects keep unknown keys; nullish means absent
// or null; optional means absent only.

var (
	zbStr      = zodlite.String
	zbNum      = zodlite.Number
	zbNullish  = zodlite.Nullish
	zbOptional = zodlite.Optional
	zbArr      = zodlite.Array
	zbObj      = zodlite.Object
	zbUnion    = zodlite.Union
	zbF        = zodlite.F
	zbNStr     = zbNullish(zbStr)

	zbReasoningContent = zbUnion(
		zbObj(zbF("reasoningText", zbObj(zbF("signature", zbNStr), zbF("text", zbStr)))),
		zbObj(zbF("redactedReasoning", zbObj(zbF("data", zbStr)))),
		zbObj(zbF("redactedContent", zbStr)),
	)

	zbConverseBlock = zbObj(
		zbF("text", zbNStr),
		zbF("citationsContent", zbNullish(zbObj(zbF("content", zbNullish(zbArr(zbObj(zbF("text", zbNStr)))))))),
		zbF("toolUse", zbNullish(zbObj(zbF("toolUseId", zbStr), zbF("name", zbStr), zbF("input", zodlite.Unknown)))),
		zbF("reasoningContent", zbNullish(zbReasoningContent)),
	)

	zbConverse = zbObj(
		zbF("metrics", zbNullish(zbObj(zbF("latencyMs", zbNum)))),
		zbF("output", zbObj(zbF("message", zbObj(
			zbF("content", zbArr(zbConverseBlock)),
			zbF("role", zbStr),
		)))),
		zbF("stopReason", zbStr),
		zbF("additionalModelResponseFields", zbNullish(zbObj(zbF("delta", zbNullish(zbObj(zbF("stop_sequence", zbNStr))))))),
		zbF("trace", zodlite.Unknown),
		zbF("performanceConfig", zbNullish(zbObj(zbF("latency", zbStr)))),
		zbF("serviceTier", zbNullish(zbObj(zbF("type", zbStr)))),
		zbF("usage", zbObj(
			zbF("inputTokens", zbNum), zbF("outputTokens", zbNum), zbF("totalTokens", zbNum),
			zbF("cacheReadInputTokens", zbNullish(zbNum)), zbF("cacheWriteInputTokens", zbNullish(zbNum)),
			zbF("cacheDetails", zbNullish(zbArr(zbObj(zbF("inputTokens", zbNum), zbF("ttl", zbStr))))),
		)),
	)

	zbEmbedding = zbUnion(
		// Titan
		zbObj(zbF("embedding", zbArr(zbNum)), zbF("inputTextTokenCount", zbNum)),
		// Nova
		zbObj(zbF("embeddings", zbArr(zbObj(zbF("embeddingType", zbStr), zbF("embedding", zbArr(zbNum))))),
			zbF("inputTokenCount", zbOptional(zbNum))),
		// Cohere v3
		zbObj(zbF("embeddings", zbArr(zbArr(zbNum)))),
		// Cohere v4
		zbObj(zbF("embeddings", zbObj(zbF("float", zbArr(zbArr(zbNum)))))),
	)

	zbImage = zbObj(
		zbF("images", zbOptional(zbArr(zbStr))),
		zbF("id", zbOptional(zbStr)), zbF("status", zbOptional(zbStr)),
		zbF("result", zodlite.Unknown), zbF("progress", zodlite.Unknown),
		zbF("details", zbOptional(zodlite.Record)),
		zbF("preview", zodlite.Unknown),
	)

	zbRerank = zbObj(
		zbF("results", zbArr(zbObj(zbF("index", zbNum), zbF("relevanceScore", zbNum)))),
		zbF("nextToken", zbOptional(zbStr)),
	)
)

// bedrockValidator is the check for a 2xx body against one of the schemas
// above (TS createJsonResponseHandler(schema)).
func bedrockValidator(schema zodlite.Node) func(int, http.Header, []byte) error {
	return providerutils.NodeResponseValidator("amazon-bedrock", schema)
}
