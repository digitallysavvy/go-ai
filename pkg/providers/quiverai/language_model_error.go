package quiverai

import (
	"encoding/json"
	"math"
	"net/http"

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
	apiErr, ok := parseQuiverAIErrorBody(statusErr.Body)
	if ok {
		perr := providererrors.NewProviderError("quiverai", statusErr.StatusCode, apiErr.Code, apiErr.Message, nil)
		perr.Data = apiErr
		perr.ResponseBody = string(statusErr.Body)
		perr.Retryable = quiverAIRetryable(statusErr.StatusCode)
		return perr
	}
	perr := providererrors.NewProviderError("quiverai", statusErr.StatusCode, "", http.StatusText(statusErr.StatusCode), nil)
	perr.ResponseBody = string(statusErr.Body)
	perr.Retryable = quiverAIRetryable(statusErr.StatusCode)
	return perr
}

// quiverAIRetryable is the TS quiveraiFailedResponseHandler isRetryable:
// only 429 and 5xx, unlike the APICallError default that also retries 408
// and 409.
func quiverAIRetryable(status int) *bool {
	retryable := status == http.StatusTooManyRequests || status >= 500
	return &retryable
}

// parseQuiverAIErrorBody validates body against the TS quiveraiErrorSchema:
// status is an integer and code, message and request_id are non-empty
// strings. Anything else (empty body, non-JSON, a partial object) is not an
// API error body, and TS createJsonErrorResponseHandler then uses the HTTP
// status text as the message.
func parseQuiverAIErrorBody(body []byte) (quiverAPIError, bool) {
	var raw struct {
		Status    *float64 `json:"status"`
		Code      *string  `json:"code"`
		Message   *string  `json:"message"`
		RequestID *string  `json:"request_id"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return quiverAPIError{}, false
	}
	if raw.Status == nil || *raw.Status != math.Trunc(*raw.Status) ||
		raw.Code == nil || *raw.Code == "" ||
		raw.Message == nil || *raw.Message == "" ||
		raw.RequestID == nil || *raw.RequestID == "" {
		return quiverAPIError{}, false
	}
	return quiverAPIError{Status: int(*raw.Status), Code: *raw.Code, Message: *raw.Message, RequestID: *raw.RequestID}, true
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
