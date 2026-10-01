package quiverai

import (
	"encoding/json"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
)

// quiverAIFailedResponseHandler maps a non-2xx HTTP response from the
// QuiverAI Responses endpoint into a *providererrors.ProviderError, using
// QuiverAI's {status,code,message,request_id} error schema (reuses the
// quiverAPIError type already defined for the image model in
// image_model.go). Mirrors the TS SDK's quiveraiFailedResponseHandler
// (quiverai-error.ts): isRetryable is classified by the HTTP response
// status (429 or >=500), not the embedded `status` field.
func quiverAIFailedResponseHandler(statusErr *internalhttp.HTTPStatusError) error {
	var apiErr quiverAPIError
	if json.Unmarshal(statusErr.Body, &apiErr) == nil && apiErr.Message != "" {
		perr := providererrors.NewProviderError("quiverai", statusErr.StatusCode, apiErr.Code, apiErr.Message, nil)
		perr.Data = apiErr
		perr.ResponseBody = string(statusErr.Body)
		return perr
	}
	perr := providererrors.NewProviderError("quiverai", statusErr.StatusCode, "", string(statusErr.Body), nil)
	perr.ResponseBody = string(statusErr.Body)
	return perr
}

// quiverAIResponseErrorMetadata extracts QuiverAI's endpoint-specific
// `error.status_code` from an embedded response error (200 response with an
// `error` field, or a streamed response.failed/error event), mirroring the
// TS SDK's getQuiverAIResponseErrorMetadata: only a well-formed HTTP status
// integer in [400, 599] is honored; isRetryable is left nil so the default
// (429/5xx) classification applies.
func quiverAIResponseErrorMetadata(respErr *openresponses.ResponseError) (statusCode *int, retryable *bool) {
	if respErr == nil || respErr.StatusCode == nil {
		return nil, nil
	}
	sc := *respErr.StatusCode
	if sc < 400 || sc > 599 {
		return nil, nil
	}
	return &sc, nil
}
