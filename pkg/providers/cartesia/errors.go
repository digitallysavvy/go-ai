package cartesia

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// cartesiaErrorPayload documents Cartesia's error envelope:
// {"error_code","title","message","request_id","doc_url"}.
type cartesiaErrorPayload struct {
	ErrorCode string `json:"error_code"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	DocURL    string `json:"doc_url"`
}

// handleError converts an HTTP error into a ProviderError, parsing
// Cartesia's error envelope when present. Matches the TypeScript SDK's
// `${title}: ${message}` message format.
func handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload cartesiaErrorPayload
		message := string(statusErr.Body)
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && payload.Title != "" && payload.Message != "" {
			message = payload.Title + ": " + payload.Message
		}
		providerErr := providererrors.NewProviderError("cartesia", statusErr.StatusCode, payload.ErrorCode, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		return providerErr
	}
	return providererrors.NewProviderError("cartesia", 0, "", err.Error(), err)
}
