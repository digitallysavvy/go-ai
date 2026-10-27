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
	status  int
	headers map[string]string
	body    []byte
}

func (c *downloadStatusCause) Error() string { return http.StatusText(c.status) }

func (c *downloadStatusCause) HTTPStatusDetails() (int, map[string]string, []byte) {
	return c.status, c.headers, c.body
}

// StatusCodeDownloadError converts the error of a provider-response media
// download (fileutil.Download*) the way TS createStatusCodeErrorResponseHandler
// does: a non-2xx HTTP response becomes a *ProviderError (the APICallError
// analog) whose message is the HTTP status text. Every other failure (URL
// validation, size limit, transport) is returned unchanged, like TS
// getFromApi.
func StatusCodeDownloadError(provider string, err error) error {
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) || dl.StatusCode < 100 || (dl.StatusCode >= 200 && dl.StatusCode < 300) {
		return err
	}
	headers := make(map[string]string, len(dl.Headers))
	for k, v := range dl.Headers {
		headers[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	cause := &downloadStatusCause{status: dl.StatusCode, headers: headers, body: dl.Body}
	return providererrors.NewProviderErrorFromResponse(provider, 0, "", http.StatusText(dl.StatusCode), cause)
}
