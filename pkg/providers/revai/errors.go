package revai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// revaiErrorPayload documents Rev.ai's error envelope: {"error":{"message","code"}}.
type revaiErrorPayload struct {
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// handleError converts an HTTP error into a ProviderError, parsing Rev.ai's
// `{"error":{"message","code"}}` error shape when present.
func handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload revaiErrorPayload
		message := string(statusErr.Body)
		code := ""
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && payload.Error.Message != "" {
			message = payload.Error.Message
			code = fmt.Sprintf("%d", payload.Error.Code)
		}
		providerErr := providererrors.NewProviderError("revai", statusErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		return providerErr
	}

	var downloadErr *providererrors.DownloadError
	if errors.As(err, &downloadErr) {
		var payload revaiErrorPayload
		message := string(downloadErr.Body)
		code := ""
		if jsonErr := json.Unmarshal(downloadErr.Body, &payload); jsonErr == nil && payload.Error.Message != "" {
			message = payload.Error.Message
			code = fmt.Sprintf("%d", payload.Error.Code)
		}
		if message == "" {
			message = downloadErr.Error()
		}
		providerErr := providererrors.NewProviderError("revai", downloadErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = flattenHeaders(downloadErr.Headers)
		providerErr.ResponseBody = string(downloadErr.Body)
		return providerErr
	}

	return providererrors.NewProviderError("revai", 0, "", err.Error(), err)
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
