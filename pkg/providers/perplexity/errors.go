package perplexity

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// parsePerplexityHTTPError extracts a perplexityErrorSchema-equivalent
// envelope from an HTTP error response body (via internalhttp.HTTPStatusError)
// and returns a ProviderError carrying the resolved message. Mirrors TS
// createJsonErrorResponseHandler({errorSchema: perplexityErrorSchema,
// errorToMessage: perplexityErrorToMessage}). If the body doesn't parse as
// JSON, err is returned unwrapped so a generic error is still produced
// upstream.
func parsePerplexityHTTPError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return err
	}

	var envelope perplexityErrorEnvelope
	if unmarshalErr := json.Unmarshal(statusErr.Body, &envelope); unmarshalErr != nil {
		return err
	}
	var raw map[string]interface{}
	_ = json.Unmarshal(statusErr.Body, &raw)

	return &providererrors.ProviderError{
		Provider:     "perplexity",
		StatusCode:   statusErr.StatusCode,
		Message:      perplexityErrorToMessage(envelope),
		Cause:        err,
		ResponseBody: string(statusErr.Body),
		Data:         raw,
	}
}
