package perplexity

import (
	"errors"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

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
	// TS createJsonErrorResponseHandler reads response.text() (BOM dropped,
	// invalid UTF-8 replaced).
	decoded := *statusErr
	decoded.Body = []byte(textdecode.UTF8(statusErr.Body))
	statusErr = &decoded
	message := http.StatusText(statusErr.StatusCode)
	var raw map[string]interface{}
	if perplexityErrorBodyMatches(statusErr.Body) {
		var envelope perplexityErrorEnvelope
		if securejson.Unmarshal(statusErr.Body, &envelope) == nil {
			message = perplexityErrorToMessage(envelope)
			_ = securejson.Unmarshal(statusErr.Body, &raw)
		}
	}
	pe := &providererrors.ProviderError{
		Provider:        providerName,
		StatusCode:      statusErr.StatusCode,
		Message:         message,
		ResponseHeaders: lowerHeaders(providerutils.ExtractHeaders(statusErr.Headers)),
		ResponseBody:    string(statusErr.Body),
	}
	if raw != nil { // TS data: only a body that matched the schema
		pe.Data = raw
	}
	return pe
}

// perplexityStatusError returns the ProviderError of a failed HTTP response
// (perplexityHTTPError) with err as its cause, and false for any other error
// (a transport failure or an already built error such as the "Invalid JSON
// response" schema failure, which must not become its own cause).
func perplexityStatusError(err error, providerName string) (*providererrors.ProviderError, bool) {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil, false
	}
	pe, ok := perplexityHTTPError(err, providerName).(*providererrors.ProviderError)
	if !ok {
		return nil, false
	}
	pe.Cause = err
	return pe, true
}
