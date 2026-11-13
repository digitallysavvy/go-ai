package revai

import (
	"errors"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"math"
	"net/http"

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
		message, code := errorMessageAndCodeText(statusErr.Text(), statusErr.Body)
		perr := providererrors.NewProviderErrorFromResponse("revai", 0, code, message, err)
		perr.Data = revaiErrorData(statusErr.Body)
		return perr
	}

	var downloadErr *providererrors.DownloadError
	if errors.As(err, &downloadErr) && downloadErr.StatusCode != 0 {
		// A handler message is final, an empty one included (TS APICallError
		// keeps ""). A download failure with no status never reached an HTTP
		// response and propagates unchanged below.
		// TS reads the failed body as text (TextDecoder).
		body := []byte(textdecode.UTF8(downloadErr.Body))
		message, code := errorMessageAndCodeText(downloadErr.StatusText, body)
		providerErr := providererrors.NewProviderError("revai", downloadErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = flattenHeaders(downloadErr.Headers)
		providerErr.ResponseBody = string(body)
		providerErr.Data = revaiErrorData(body)
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
	return errorMessageAndCodeText(http.StatusText(statusCode), body)
}

// errorMessageAndCodeText is errorMessageAndCode with the response's own
// status text (TS response.statusText, the server's reason phrase).
func errorMessageAndCodeText(statusText string, body []byte) (message, code string) {
	var payload revaiErrorPayload
	if err := securejson.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil && !math.IsInf(*payload.Error.Code, 0) && !math.IsNaN(*payload.Error.Code) { // z.number() rejects Infinity
		return *payload.Error.Message, providerutils.JSNumberString(*payload.Error.Code)
	}
	return statusText, ""
}

// revaiErrorData is TS APICallError.data: the revaiErrorDataSchema output
// ({error: {message, code}}, other keys stripped) for a matching body, else
// nil.
func revaiErrorData(body []byte) interface{} {
	var payload revaiErrorPayload
	if err := securejson.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil && !math.IsInf(*payload.Error.Code, 0) && !math.IsNaN(*payload.Error.Code) { // z.number() rejects Infinity
		return map[string]interface{}{"error": map[string]interface{}{"message": *payload.Error.Message, "code": *payload.Error.Code}}
	}
	return nil
}
