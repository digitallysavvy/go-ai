package googlevertex

import (
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// vertexErrorData is TS googleVertexErrorDataSchema (google-vertex-error.ts):
// {error: {code: number | null, message: string, status: string}}. Unlike the
// Google schema it has no `details`, so zod strips them (any type) from the
// error data.
type vertexErrorData struct {
	Error struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// vertexHTTPError builds the ProviderError for a failed response handled by
// TS googleVertexFailedResponseHandler: the message is error.message when the
// body matches googleVertexErrorDataSchema, otherwise the HTTP status text.
func vertexHTTPError(providerName string, status int, headers http.Header, body []byte, cause error) *providererrors.ProviderError {
	// TS createJsonErrorResponseHandler reads response.text().
	body = []byte(textdecode.UTF8(body))
	message := http.StatusText(status)
	if message == "" && cause != nil {
		message = cause.Error()
	}
	var raw struct {
		Error *struct {
			Code    json.RawMessage `json:"code"`
			Message *string         `json:"message"`
			Status  *string         `json:"status"`
		} `json:"error"`
	}
	var data vertexErrorData
	schemaOK := securejson.Unmarshal(body, &raw) == nil && raw.Error != nil && raw.Error.Message != nil && raw.Error.Status != nil && len(raw.Error.Code) > 0
	if schemaOK {
		var code *float64
		if json.Unmarshal(raw.Error.Code, &code) != nil {
			schemaOK = false
		} else {
			if code != nil {
				c := int(*code)
				data.Error.Code = &c
			}
			data.Error.Message = *raw.Error.Message
			data.Error.Status = *raw.Error.Status
			message = data.Error.Message
		}
	}
	if !schemaOK {
		data = vertexErrorData{}
	}
	perr := providererrors.NewProviderError(providerName, status, data.Error.Status, message, cause)
	perr.ResponseBody = string(body)
	if len(headers) > 0 {
		perr.ResponseHeaders = providerutils.ExtractHeaders(headers)
	}
	if schemaOK {
		perr.Data = data
	}
	return perr
}

// vertexHandleError maps a failed internal-client call of a model whose TS
// failedResponseHandler is googleVertexFailedResponseHandler.
func vertexHandleError(providerName string, err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
	if providerutils.IsInvalidJSONResponse(err) {
		return err // TS createJsonResponseHandler failure is an APICallError as is
	}
	var loadKeyErr *providererrors.LoadAPIKeyError
	if errors.As(err, &loadKeyErr) {
		return err
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return vertexHTTPError(providerName, statusErr.StatusCode, statusErr.Headers, statusErr.Body, err)
	}
	return providererrors.NewProviderError(providerName, 0, "", err.Error(), err)
}
