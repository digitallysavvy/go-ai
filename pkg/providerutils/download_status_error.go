package providerutils

import (
	"errors"
	"net/http"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// downloadStatusCause carries a failed download's HTTP response so the
// ProviderError built from it has the status code, headers and body (TS
// APICallError statusCode, responseHeaders, responseBody).
type downloadStatusCause struct {
	status     int
	statusText string
	headers    map[string]string
	body       []byte
}

func (c *downloadStatusCause) Error() string { return c.statusText }

func (c *downloadStatusCause) HTTPStatusDetails() (int, map[string]string, []byte) {
	return c.status, c.headers, c.body
}

// StatusCodeDownloadError converts the error of a provider-response media
// download (fileutil.Download*) the way TS createStatusCodeErrorResponseHandler
// does: a non-2xx HTTP response becomes a *ProviderError (the APICallError
// analog) whose message is the response status text. Every other failure (URL
// validation, size limit, transport) is returned unchanged, like TS
// getFromApi.
func StatusCodeDownloadError(provider string, err error) error {
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) || dl.StatusCode < 100 || (dl.StatusCode >= 200 && dl.StatusCode < 300) {
		return err
	}
	headers := make(map[string]string, len(dl.Headers))
	for k, v := range dl.Headers {
		headers[strings.ToLower(k)] = IsomorphicDecode(FetchHeaderEntry(k, v))
	}
	// TS message: response.statusText, the server's reason phrase
	// (response-handler.ts:459); the Go status text only when the response
	// had none (r91 core2-06).
	statusText := dl.StatusText
	if statusText == "" {
		statusText = http.StatusText(dl.StatusCode)
	}
	cause := &downloadStatusCause{status: dl.StatusCode, statusText: statusText, headers: headers, body: dl.Body}
	pe := providererrors.NewProviderErrorFromResponse(provider, 0, "", statusText, cause)
	// TS createStatusCodeErrorResponseHandler builds the APICallError
	// without data, and its message is the status text even when the body
	// is JSON with an error message (the cause's text equals the message,
	// which NewProviderErrorFromResponse would replace from the body).
	pe.Data = nil
	pe.Message = statusText
	return pe
}
