package revai

import (
	"errors"
	"net/http"
	"strconv"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

// flattenHeaders is providerutils.ExtractHeaders (TS extractResponseHeaders:
// repeated values joined, the last Set-Cookie line, each byte decoded as one
// character; r91 provc-06).
func flattenHeaders(h map[string][]string) map[string]string {
	return providerutils.ExtractHeaders(h)
}

// errorMessageAndCode mirrors the TS revaiFailedResponseHandler: a body matching
// {"error":{"message":string,"code":number}} gives that message (even an
// empty one) and the code; anything else (empty, not JSON, a missing or
// wrongly typed field) gives the HTTP status text.
func errorMessageAndCode(statusCode int, body []byte) (message, code string) {
	var payload revaiErrorPayload
	if err := securejson.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil {
		return *payload.Error.Message, strconv.FormatFloat(*payload.Error.Code, 'f', -1, 64)
	}
	return http.StatusText(statusCode), ""
}
