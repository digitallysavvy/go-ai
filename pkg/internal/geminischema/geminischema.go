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
		// union([{webDynamicRetrievalScore: number}, {}]): any object passes,
		// and a non-object reports both members.
		fl("retrievalMetadata", zNullish(zUnionOf(zObj(fl("webDynamicRetrievalScore", zNum)), zObj()))),
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

	zContent = zObj(fl("parts", zNullish(zArr(zPart))))

	// Response is the streaming chunkSchema: it also checks an in-stream
	// `error` frame.
	Response = response(fl("error", zNullish(zGoogleError)), fl("content", zNullish(zContent)))

	// GenerateResponse is responseSchema (doGenerate and google-batch result
	// lines). It has no `error` key, so TS strips one of any shape, and its
	// content is getContentSchema().nullish().or(z.object({}).strict()).
	GenerateResponse = response(fl("content", zUnionOf(zNullish(zContent), zodlite.StrictEmptyObject)))
)

// response builds the top-level GenerateContentResponse object. extra goes
// first (the streaming `error` key) and the last field is the candidate's
// content.
func response(fields ...zodlite.Field) zodlite.Node {
	extra, content := fields[:len(fields)-1], fields[len(fields)-1]
	return zObj(append(append([]zodlite.Field{}, extra...),
		fl("responseId", zNullish(zStr)),
		fl("candidates", zNullish(zArr(zObj(
			content,
			fl("finishReason", zNullish(zStr)), fl("finishMessage", zNullish(zStr)), fl("continuationToken", zNullish(zStr)),
			fl("safetyRatings", zNullish(zArr(zSafety))),
			fl("groundingMetadata", zNullish(zGrounding)),
			fl("urlContextMetadata", zNullish(zObj(fl("urlMetadata", zNullish(zArr(zObj(fl("retrievedUrl", zStr), fl("urlRetrievalStatus", zStr)))))))),
		)))),
		fl("usageMetadata", zNullish(zUsage)),
		fl("promptFeedback", zNullish(zObj(fl("blockReason", zNullish(zStr)), fl("safetyRatings", zNullish(zArr(zSafety)))))),
	)...)
}

// ValidationMessage validates a streaming frame against Response (TS
// chunkSchema). It returns "" when it matches (or is not JSON), otherwise the
// TS TypeValidationError message.
func ValidationMessage(data string) string {
	return zodlite.ValidationMessage(Response, data)
}

// GenerateValidationMessage validates a doGenerate body or batch result line
// against GenerateResponse (TS responseSchema).
func GenerateValidationMessage(data string) string {
	return zodlite.ValidationMessage(GenerateResponse, data)
}
