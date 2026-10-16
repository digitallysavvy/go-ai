package google

import (
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// This file ports the zod response schemas that the TS Google provider hands
// to createJsonResponseHandler. A 2xx body that does not match is an
// APICallError "Invalid JSON response" caused by a TypeValidationError.
// Objects keep unknown keys; nullish means absent or null.

var (
	zStr     = zodlite.String
	zNum     = zodlite.Number
	zBool    = zodlite.Boolean
	zNullish = zodlite.Nullish
	zArr     = zodlite.Array
	zObj     = zodlite.Object
	zUnionOf = zodlite.Union
	fl       = zodlite.F
)

var (
	nStr = zNullish(zStr)

	// googleGenerativeAITextEmbeddingResponseSchema and
	// googleGenerativeAISingleEmbeddingResponseSchema (google-embedding-model.ts).
	zGoogleBatchEmbedding  = zObj(fl("embeddings", zArr(zObj(fl("values", zArr(zNum))))))
	zGoogleSingleEmbedding = zObj(fl("embedding", zObj(fl("values", zArr(zNum)))))

	// googleOperationSchema (google-video-model.ts).
	zGoogleOperation = zObj(
		fl("name", nStr), fl("done", zNullish(zBool)),
		fl("error", zNullish(zObj(fl("code", zNullish(zNum)), fl("message", zStr), fl("status", nStr)))),
		fl("response", zNullish(zObj(fl("generateVideoResponse", zNullish(zObj(
			fl("generatedSamples", zNullish(zArr(zObj(fl("video", zNullish(zObj(fl("uri", nStr)))))))),
		)))))),
	)

	// googleSpeechResponseSchema (google-speech-api.ts).
	zGoogleSpeech = zObj(
		fl("candidates", zNullish(zArr(zObj(fl("content", zNullish(zObj(
			fl("parts", zNullish(zArr(zObj(fl("inlineData", zNullish(zObj(fl("mimeType", nStr), fl("data", nStr)))))))),
		))))))),
		fl("usageMetadata", zNullish(zodlite.Record)),
	)

	// googleInteractionsTranscriptionResponseSchema (transcription/google-transcription-model.ts).
	zGoogleInteractionsTranscription = zObj(
		fl("status", nStr),
		fl("steps", zNullish(zArr(zObj(
			fl("type", nStr),
			fl("content", zNullish(zArr(zObj(
				fl("type", nStr), fl("text", nStr),
				fl("annotations", zNullish(zArr(zObj(
					fl("type", nStr), fl("text", nStr), fl("speaker", nStr),
					fl("start_offset", nStr), fl("end_offset", nStr),
				)))),
			)))),
		)))),
		fl("usage", zNullish(zodlite.Record)),
	)

	// googleFileResponseSchema (google-files.ts), applied to the processing poll.
	zGoogleFile = zObj(
		fl("name", zStr), fl("displayName", nStr), fl("mimeType", zStr), fl("sizeBytes", nStr),
		fl("createTime", nStr), fl("updateTime", nStr), fl("expirationTime", nStr), fl("sha256Hash", nStr),
		fl("uri", zStr), fl("state", zStr),
	)

	zGoogleRPCStatus = zObj(
		fl("code", zNullish(zUnionOf(zNum, zStr))), fl("message", nStr), fl("status", nStr),
	)
	zGoogleBatchCount = zNullish(zUnionOf(zStr, zNum))
	zGoogleBatchStats = zObj(
		fl("requestCount", zGoogleBatchCount), fl("successfulRequestCount", zGoogleBatchCount),
		fl("failedRequestCount", zGoogleBatchCount), fl("pendingRequestCount", zGoogleBatchCount),
	)
	zGoogleBatchOutput = zObj(
		fl("responsesFile", nStr),
		fl("inlinedResponses", zNullish(zObj(fl("inlinedResponses", zArr(zObj(
			fl("metadata", zObj(fl("key", zStr))),
			fl("response", zodlite.Unknown),
			fl("error", zNullish(zGoogleRPCStatus)),
		)))))),
	)
	// googleBatchOperationZodSchema (google-batch.ts).
	zGoogleBatchOperation = zObj(
		fl("name", zStr),
		fl("metadata", zNullish(zObj(
			fl("state", nStr), fl("createTime", nStr),
			fl("batchStats", zNullish(zGoogleBatchStats)), fl("output", zNullish(zGoogleBatchOutput)),
		))),
		fl("done", zNullish(zBool)),
		fl("error", zNullish(zGoogleRPCStatus)),
		fl("response", zNullish(zGoogleBatchOutput)),
	)
	zGoogleBatchList = zObj(
		fl("operations", zNullish(zArr(zGoogleBatchOperation))), fl("nextPageToken", nStr),
	)
	zGoogleBatchCancel = zObj()
	// googleFileUploadResponseSchema (google-batch.ts).
	zGoogleBatchFileUpload = zObj(fl("file", zObj(fl("name", zStr), fl("expirationTime", nStr))))

	// googleInteractionsResponseSchema (interactions/google-interactions-api.ts).
	zGoogleInteractions = googleInteractionsResponseNode()
)

func googleInteractionsResponseNode() zodlite.Node {
	loose := func(typ string, fields ...zodlite.Field) zodlite.Node {
		return zodlite.Typed(typ, fields...)
	}
	anyTyped := zObj(fl("type", zStr))
	tokenByModality := zNullish(zArr(zObj(fl("modality", nStr), fl("tokens", zNullish(zNum)))))
	usage := zObj(
		fl("total_input_tokens", zNullish(zNum)), fl("total_output_tokens", zNullish(zNum)),
		fl("total_thought_tokens", zNullish(zNum)), fl("total_cached_tokens", zNullish(zNum)),
		fl("total_tool_use_tokens", zNullish(zNum)), fl("total_tokens", zNullish(zNum)),
		fl("input_tokens_by_modality", tokenByModality), fl("output_tokens_by_modality", tokenByModality),
		fl("cached_tokens_by_modality", tokenByModality), fl("tool_use_tokens_by_modality", tokenByModality),
		fl("grounding_tool_count", zNullish(zArr(zObj(fl("type", nStr), fl("count", zNullish(zNum)))))),
	)
	annotation := zUnionOf(
		loose("url_citation", fl("url", nStr), fl("title", nStr), fl("start_index", zNullish(zNum)), fl("end_index", zNullish(zNum))),
		loose("file_citation", fl("file_name", nStr), fl("document_uri", nStr), fl("url", nStr), fl("page_number", zNullish(zNum)),
			fl("media_id", nStr), fl("start_index", zNullish(zNum)), fl("end_index", zNullish(zNum)),
			fl("custom_metadata", zNullish(zodlite.Record))),
		loose("place_citation", fl("name", nStr), fl("url", nStr), fl("place_id", nStr),
			fl("start_index", zNullish(zNum)), fl("end_index", zNullish(zNum))),
		anyTyped,
	)
	resolutions := zodlite.Enum("low", "medium", "high", "ultra_high")
	block := zUnionOf(
		loose("text", fl("text", zStr), fl("annotations", zNullish(zArr(annotation)))),
		loose("image", fl("data", nStr), fl("mime_type", nStr), fl("resolution", zNullish(resolutions)), fl("uri", nStr)),
		loose("video", fl("data", nStr), fl("mime_type", nStr), fl("uri", nStr)),
		anyTyped,
	)
	summaryItem := zObj(fl("type", zStr), fl("text", nStr), fl("data", nStr), fl("mime_type", nStr))
	args := zNullish(zodlite.Record)
	step := zUnionOf(
		loose("user_input", fl("content", zNullish(zArr(block)))),
		loose("model_output", fl("content", zNullish(zArr(block)))),
		loose("function_call", fl("id", zStr), fl("name", zStr), fl("arguments", args), fl("signature", nStr)),
		loose("thought", fl("signature", nStr), fl("summary", zNullish(zArr(summaryItem)))),
		loose("processing_call", fl("id", zStr), fl("signature", nStr)),
		loose("processing_result", fl("call_id", zStr), fl("signature", nStr)),
		zObj(fl("type", zodlite.Enum("google_search_call", "code_execution_call", "url_context_call",
			"file_search_call", "google_maps_call", "mcp_server_tool_call")),
			fl("id", zStr), fl("arguments", args), fl("name", nStr), fl("server_name", nStr),
			fl("search_type", nStr), fl("signature", nStr)),
		zObj(fl("type", zodlite.Enum("google_search_result", "code_execution_result", "url_context_result",
			"file_search_result", "google_maps_result", "mcp_server_tool_result")),
			fl("call_id", zStr), fl("result", zodlite.Unknown), fl("is_error", zNullish(zBool)),
			fl("name", nStr), fl("server_name", nStr), fl("signature", nStr)),
		anyTyped,
	)
	return zObj(
		fl("id", nStr), fl("created", nStr), fl("updated", nStr),
		fl("status", zodlite.Enum("in_progress", "requires_action", "completed", "failed", "cancelled", "incomplete")),
		fl("model", nStr), fl("agent", nStr),
		fl("steps", zNullish(zArr(step))),
		fl("usage", zNullish(usage)),
		fl("service_tier", nStr), fl("previous_interaction_id", nStr),
		fl("response_modalities", zNullish(zArr(zStr))),
	)
}

// googleValidator is the Request.ValidateResponse check for one of the
// schemas above (TS createJsonResponseHandler(schema)).
func googleValidator(schema zodlite.Node) func(int, http.Header, []byte) error {
	return providerutils.NodeResponseValidator("google", schema)
}
