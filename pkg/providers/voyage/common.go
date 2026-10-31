package voyage

import (
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// handleError wraps a transport/API error as a ProviderError, leaving the
// errors TS throws unwrapped (LoadAPIKeyError, the "Invalid JSON response"
// APICallError) untouched.
func handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) || providerutils.IsInvalidJSONResponse(err) {
		return err
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return providererrors.NewProviderErrorFromResponse("voyage", 0, "", voyageErrorMessage(statusErr.StatusCode, statusErr.Body), err)
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// voyageErrorMessage mirrors TS voyageFailedResponseHandler: {"detail":string}
// gives the detail, any other body gives the HTTP status text.
func voyageErrorMessage(statusCode int, body []byte) string {
	var payload struct {
		Detail *string `json:"detail"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Detail != nil {
		return *payload.Detail
	}
	return http.StatusText(statusCode)
}

func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}
