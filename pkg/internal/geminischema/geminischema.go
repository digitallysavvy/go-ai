// Package geminischema ports the zod chunkSchema / responseSchema
// (google-language-model.ts) that TS applies to every Gemini/Vertex response
// frame and google-batch.ts to every batch result line. It is internal so the
// schema stays out of the public API.
package geminischema

import "github.com/digitallysavvy/go-ai/pkg/internal/zodlite"

var (
	zStr     = zodlite.String
	zNum     = zodlite.Number
	zBool    = zodlite.Boolean
	zAny     = zodlite.Unknown
	zNullish = zodlite.Nullish
	zArr     = zodlite.Array
	zObj     = zodlite.Object
	zUnionOf = zodlite.Union
	fl       = zodlite.F
)

var (
	zSig         = fl("thoughtSignature", zNullish(zStr))
	zTokenDetail = zNullish(zArr(zObj(fl("modality", zStr), fl("tokenCount", zNum))))

	zPart = zUnionOf(
		// note: order matters in TS since text can be fully empty
		zObj(fl("functionCall", zObj(
			fl("id", zNullish(zStr)), fl("name", zNullish(zStr)), fl("args", zAny),
			fl("partialArgs", zNullish(zArr(zObj(
				fl("jsonPath", zStr), fl("stringValue", zNullish(zStr)), fl("numberValue", zNullish(zNum)),
				fl("boolValue", zNullish(zBool)), fl("nullValue", zAny), fl("willContinue", zNullish(zBool)),
			)))),
			fl("willContinue", zNullish(zBool)),
		)), zSig),
		zObj(fl("inlineData", zObj(fl("mimeType", zStr), fl("data", zStr))), fl("thought", zNullish(zBool)), zSig),
		zObj(fl("toolCall", zObj(fl("toolType", zStr), fl("args", zAny), fl("id", zStr))), zSig),
		zObj(fl("toolResponse", zObj(fl("toolType", zStr), fl("response", zAny), fl("id", zStr))), zSig),
		zObj(
			fl("executableCode", zNullish(zObj(fl("language", zStr), fl("code", zStr)))),
			fl("codeExecutionResult", zNullish(zObj(fl("outcome", zStr), fl("output", zNullish(zStr))))),
			fl("text", zNullish(zStr)), fl("thought", zNullish(zBool)), zSig,
		),
	)

	zSafety = zObj(
		fl("category", zNullish(zStr)), fl("probability", zNullish(zStr)), fl("probabilityScore", zNullish(zNum)),
		fl("severity", zNullish(zStr)), fl("severityScore", zNullish(zNum)), fl("blocked", zNullish(zBool)),
	)

	zStrArr = zNullish(zArr(zStr))
	zNumArr = zNullish(zArr(zNum))

	zGrounding = zObj(
		fl("webSearchQueries", zStrArr), fl("imageSearchQueries", zStrArr), fl("retrievalQueries", zStrArr),
		fl("searchEntryPoint", zNullish(zObj(fl("renderedContent", zStr)))),
		fl("groundingChunks", zNullish(zArr(zObj(
			fl("web", zNullish(zObj(fl("uri", zStr), fl("title", zNullish(zStr))))),
			fl("image", zNullish(zObj(fl("sourceUri", zStr), fl("imageUri", zStr), fl("title", zNullish(zStr)), fl("domain", zNullish(zStr))))),
			fl("retrievedContext", zNullish(zObj(
				fl("uri", zNullish(zStr)), fl("title", zNullish(zStr)), fl("text", zNullish(zStr)), fl("fileSearchStore", zNullish(zStr)),
				fl("customMetadata", zNullish(zArr(zObj(
					fl("key", zStr), fl("stringValue", zNullish(zStr)), fl("numericValue", zNullish(zNum)),
					fl("stringListValue", zNullish(zObj(fl("values", zStrArr)))),
				)))),
			))),
			fl("maps", zNullish(zObj(fl("uri", zNullish(zStr)), fl("title", zNullish(zStr)), fl("text", zNullish(zStr)), fl("placeId", zNullish(zStr))))),
		)))),
		fl("groundingSupports", zNullish(zArr(zObj(
			fl("segment", zNullish(zObj(fl("startIndex", zNullish(zNum)), fl("endIndex", zNullish(zNum)), fl("text", zNullish(zStr))))),
			fl("segment_text", zNullish(zStr)), fl("groundingChunkIndices", zNumArr), fl("supportChunkIndices", zNumArr),
			fl("confidenceScores", zNumArr), fl("confidenceScore", zNumArr),
		)))),
		// union([{webDynamicRetrievalScore: number}, {}]): any object passes.
		fl("retrievalMetadata", zNullish(zObj())),
	)

	zUsage = zObj(
		fl("cachedContentTokenCount", zNullish(zNum)), fl("thoughtsTokenCount", zNullish(zNum)),
		fl("promptTokenCount", zNullish(zNum)), fl("candidatesTokenCount", zNullish(zNum)),
		fl("toolUsePromptTokenCount", zNullish(zNum)), fl("totalTokenCount", zNullish(zNum)),
		fl("trafficType", zNullish(zStr)), fl("serviceTier", zNullish(zStr)),
		fl("promptTokensDetails", zTokenDetail), fl("cacheTokensDetails", zTokenDetail),
		fl("candidatesTokensDetails", zTokenDetail), fl("toolUsePromptTokensDetails", zTokenDetail),
	)

	// getGoogleErrorSchema (google-error.ts): the `error` object of an in-stream
	// error frame; `code` is required but nullable.
	zGoogleError = zObj(
		fl("code", zodlite.Nullable(zNum)), fl("message", zStr), fl("status", zStr),
		fl("details", zNullish(zArr(zAny))),
	)

	Response = zObj(append([]zodlite.Field{fl("error", zNullish(zGoogleError))}, responseFields...)...)

	// BatchResponse is TS responseSchema (batch results): the same object
	// without the in-stream `error` key, which zod strips.
	BatchResponse = zObj(responseFields...)

	responseFields = []zodlite.Field{
		fl("responseId", zNullish(zStr)),
		fl("candidates", zNullish(zArr(zObj(
			fl("content", zNullish(zObj(fl("parts", zNullish(zArr(zPart)))))),
			fl("finishReason", zNullish(zStr)), fl("finishMessage", zNullish(zStr)), fl("continuationToken", zNullish(zStr)),
			fl("safetyRatings", zNullish(zArr(zSafety))),
			fl("groundingMetadata", zNullish(zGrounding)),
			fl("urlContextMetadata", zNullish(zObj(fl("urlMetadata", zNullish(zArr(zObj(fl("retrievedUrl", zStr), fl("urlRetrievalStatus", zStr)))))))),
		)))),
		fl("usageMetadata", zNullish(zUsage)),
		fl("promptFeedback", zNullish(zObj(fl("blockReason", zNullish(zStr)), fl("safetyRatings", zNullish(zArr(zSafety)))))),
	}
)

// ValidationMessage validates a GenerateContent JSON body or frame against
// Response. It returns "" when it matches (or is not JSON), otherwise the TS
// TypeValidationError message.
func ValidationMessage(data string) string {
	return zodlite.ValidationMessage(Response, data)
}

// BatchValidationMessage validates a batch result against BatchResponse.
func BatchValidationMessage(data string) string {
	return zodlite.ValidationMessage(BatchResponse, data)
}
