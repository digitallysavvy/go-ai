package gemini

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
)

// This file ports the zod `chunkSchema` / `responseSchema`
// (google-language-model.ts) that TS applies to every parsed Gemini/Vertex
// response frame. The validation primitives live in pkg/internal/zodlite. A
// frame that does not match becomes an `error` part (TypeValidationError in
// TS) and the stream keeps reading.

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

	zChunk = zObj(
		fl("error", zNullish(zGoogleError)),
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
	)
)

// chunkValidationMessage validates a JSON frame against the TS chunkSchema.
// It returns "" when the frame matches (or is not JSON, which is reported
// separately), otherwise the TS TypeValidationError message.
func chunkValidationMessage(data string) string {
	return zodlite.ValidationMessage(zChunk, data)
}

// validationIssueText returns the issue list that follows "Error message: ".
func validationIssueText(message string) string { return zodlite.IssueText(message) }

func compactJSONString(data string) string { return zodlite.CompactJSON(data) }

// ResponseValidationMessage validates a GenerateContent response body against
// the TS responseSchema (the schema google-batch.ts applies to every batch
// result line with safeValidateTypes). It returns "" when the body matches.
func ResponseValidationMessage(data []byte) string {
	return chunkValidationMessage(string(data))
}
