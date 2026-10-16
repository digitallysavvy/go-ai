package hume

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message, code := errorMessageAndCode(statusErr.StatusCode, statusErr.Body)
		providerErr := providererrors.NewProviderError("hume", statusErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		return providerErr
	}
	return providererrors.NewProviderError("hume", 0, "", err.Error(), err)
}

// errorMessageAndCode mirrors the TS humeFailedResponseHandler: a body matching
// {"error":{"message":string,"code":number}} gives that message (even an
// empty one) and the code; anything else (empty, not JSON, a missing or
// wrongly typed field) gives the HTTP status text.
func errorMessageAndCode(statusCode int, body []byte) (message, code string) {
	var payload humeErrorPayload
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil {
		return *payload.Error.Message, strconv.FormatFloat(*payload.Error.Code, 'f', -1, 64)
	}
	return http.StatusText(statusCode), ""
}
