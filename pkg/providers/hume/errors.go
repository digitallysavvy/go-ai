package hume

import (
	"errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"math"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// humeErrorPayload documents Hume's error envelope: {"error":{"message","code"}}.
type humeErrorPayload struct {
	Error *struct {
		Message *string  `json:"message"`
		Code    *float64 `json:"code"`
	} `json:"error"`
}

// handleError converts an HTTP error into a ProviderError, parsing Hume's
// `{"error":{"message","code"}}` error shape when present.
func handleError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message, code := errorMessageAndCodeText(statusErr.Text(), statusErr.Body)
		perr := providererrors.NewProviderErrorFromResponse("hume", 0, code, message, err)
		perr.Data = humeErrorData(statusErr.Body)
		return perr
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// errorMessageAndCode mirrors the TS humeFailedResponseHandler: a body matching
// {"error":{"message":string,"code":number}} gives that message (even an
// empty one) and the code; anything else (empty, not JSON, a missing or
// wrongly typed field) gives the HTTP status text.
func errorMessageAndCode(statusCode int, body []byte) (message, code string) {
	return errorMessageAndCodeText(http.StatusText(statusCode), body)
}

// errorMessageAndCodeText is errorMessageAndCode with the response's reason
// phrase (TS response.statusText) as the off-schema message.
func errorMessageAndCodeText(statusText string, body []byte) (message, code string) {
	var payload humeErrorPayload
	if err := securejson.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil && !math.IsInf(*payload.Error.Code, 0) && !math.IsNaN(*payload.Error.Code) { // z.number() rejects Infinity
		return *payload.Error.Message, providerutils.JSNumberString(*payload.Error.Code)
	}
	return statusText, ""
}

// humeErrorData is TS APICallError.data: the humeErrorDataSchema output
// ({error: {message, code}}) for a matching body, else nil.
func humeErrorData(body []byte) interface{} {
	var payload humeErrorPayload
	if err := securejson.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil && !math.IsInf(*payload.Error.Code, 0) && !math.IsNaN(*payload.Error.Code) { // z.number() rejects Infinity
		return map[string]interface{}{"error": map[string]interface{}{"message": *payload.Error.Message, "code": *payload.Error.Code}}
	}
	return nil
}
