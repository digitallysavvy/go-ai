package perplexity

import (
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// perplexityHTTPError turns a failed HTTP response into a ProviderError the
// way TS createJsonErrorResponseHandler({errorSchema: perplexityErrorSchema,
// errorToMessage: perplexityErrorToMessage}) does: a body matching the schema
// gives the resolved message and its data; an empty body, non-JSON or a body
// that does not match the schema gives the HTTP status text. The status code,
// response headers and body are kept either way. Non-HTTP errors (transport,
// cancellation) pass through unchanged.
func perplexityHTTPError(err error, providerName string) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return err
	}
	message := http.StatusText(statusErr.StatusCode)
	var raw map[string]interface{}
	if perplexityErrorBodyMatches(statusErr.Body) {
		var envelope perplexityErrorEnvelope
		if json.Unmarshal(statusErr.Body, &envelope) == nil {
			message = perplexityErrorToMessage(envelope)
			_ = json.Unmarshal(statusErr.Body, &raw)
		}
	}
	return &providererrors.ProviderError{
		Provider:        providerName,
		StatusCode:      statusErr.StatusCode,
		Message:         message,
		ResponseHeaders: lowerHeaders(providerutils.ExtractHeaders(statusErr.Headers)),
		ResponseBody:    string(statusErr.Body),
		Data:            raw,
	}
}
