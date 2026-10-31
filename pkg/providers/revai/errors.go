package revai

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// revaiErrorPayload documents Rev.ai's error envelope: {"error":{"message","code"}}.
type revaiErrorPayload struct {
	Error *struct {
		Message *string  `json:"message"`
		Code    *float64 `json:"code"`
	} `json:"error"`
}

// handleError converts an HTTP error into a ProviderError, parsing Rev.ai's
// `{"error":{"message","code"}}` error shape when present.
func handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message, code := errorMessageAndCode(statusErr.StatusCode, statusErr.Body)
		return providererrors.NewProviderErrorFromResponse("revai", 0, code, message, err)
	}

	var downloadErr *providererrors.DownloadError
	if errors.As(err, &downloadErr) && downloadErr.StatusCode != 0 {
		// A handler message is final, an empty one included (TS APICallError
		// keeps ""). A download failure with no status never reached an HTTP
		// response and propagates unchanged below.
		message, code := errorMessageAndCode(downloadErr.StatusCode, downloadErr.Body)
		providerErr := providererrors.NewProviderError("revai", downloadErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = flattenHeaders(downloadErr.Headers)
		providerErr.ResponseBody = string(downloadErr.Body)
		return providerErr
	}

	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// flattenHeaders joins multi-value HTTP headers into single strings, matching
// providerutils.ExtractHeaders's convention for other providers' response
// metadata (avoids importing providerutils here for a one-line helper).
func flattenHeaders(h map[string][]string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vs := range h {
		out[http.CanonicalHeaderKey(k)] = strings.Join(vs, ", ")
	}
	return out
}

// errorMessageAndCode mirrors the TS revaiFailedResponseHandler: a body matching
// {"error":{"message":string,"code":number}} gives that message (even an
// empty one) and the code; anything else (empty, not JSON, a missing or
// wrongly typed field) gives the HTTP status text.
func errorMessageAndCode(statusCode int, body []byte) (message, code string) {
	var payload revaiErrorPayload
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil {
		return *payload.Error.Message, strconv.FormatFloat(*payload.Error.Code, 'f', -1, 64)
	}
	return http.StatusText(statusCode), ""
}
