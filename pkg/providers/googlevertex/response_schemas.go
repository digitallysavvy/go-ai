package googlevertex

import (
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// This file ports the zod response schemas that the TS Google Vertex provider
// hands to createJsonResponseHandler. A 2xx body that does not match is an
// APICallError "Invalid JSON response" caused by a TypeValidationError.
// Objects keep unknown keys; nullish means absent or null.

var (
	zStr     = zodlite.String
	zNum     = zodlite.Number
	zBool    = zodlite.Boolean
	zNullish = zodlite.Nullish
	zArr     = zodlite.Array
	zObj     = zodlite.Object
	fl       = zodlite.F
	nStr     = zNullish(zStr)

	// googleVertexTextEmbeddingResponseSchema.
	zVertexTextEmbedding = zObj(fl("predictions", zArr(zObj(fl("embeddings", zObj(
		fl("values", zArr(zNum)),
		fl("statistics", zObj(fl("token_count", zNum))),
	))))))

	// googleVertexEmbedContentResponseSchema.
	zVertexEmbedContent = zObj(
		fl("embedding", zObj(fl("values", zArr(zNum)))),
		fl("usageMetadata", zNullish(zObj(fl("promptTokenCount", zNullish(zNum))))),
	)

	// googleVertexOperationSchema (google-vertex-video-model.ts).
	zVertexOperation = zObj(
		fl("name", nStr), fl("done", zNullish(zBool)),
		fl("error", zNullish(zObj(fl("code", zNullish(zNum)), fl("message", zStr), fl("status", nStr)))),
		fl("response", zNullish(zObj(
			fl("videos", zNullish(zArr(zObj(
				fl("bytesBase64Encoded", nStr), fl("gcsUri", nStr), fl("mimeType", nStr),
			)))),
			fl("raiMediaFilteredCount", zNullish(zNum)),
		))),
	)

	// googleVertexCloudTTSResponseSchema.
	zVertexCloudTTS = zObj(fl("audioContent", nStr))

	// googleVertexTranscriptionResponseSchema (Cloud Speech-to-Text).
	zVertexTranscription = zObj(
		fl("results", zNullish(zArr(zObj(
			fl("alternatives", zNullish(zArr(zObj(
				fl("transcript", nStr),
				fl("words", zNullish(zArr(zObj(fl("word", nStr), fl("startOffset", nStr), fl("endOffset", nStr))))),
			)))),
			fl("languageCode", nStr),
		)))),
		fl("metadata", zNullish(zObj(fl("totalBilledDuration", nStr)))),
	)

	// googleVertexGeminiTranscriptionResponseSchema.
	zVertexGeminiWord = zObj(fl("word", nStr), fl("startOffset", nStr), fl("endOffset", nStr))
	zVertexGeminiPart = zObj(
		fl("text", nStr),
		fl("audioTranscription", zNullish(zObj(
			fl("text", nStr), fl("languageCode", nStr), fl("speakerLabel", nStr),
			fl("words", zNullish(zArr(zVertexGeminiWord))),
		))),
	)
	zVertexGeminiTranscription = zObj(
		fl("candidates", zNullish(zArr(zObj(
			fl("content", zNullish(zObj(fl("parts", zNullish(zArr(zVertexGeminiPart)))))),
		)))),
		fl("usageMetadata", zNullish(zodlite.Record)),
	)
)

// vertexValidator is the ValidateResponse check for one of the schemas above
// (TS createJsonResponseHandler(schema)).
func vertexValidator(providerName string, schema zodlite.Node) func(int, http.Header, []byte) error {
	return providerutils.NodeResponseValidator(providerName, schema)
}
