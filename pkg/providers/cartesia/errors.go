package cartesia

import (
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// cartesiaErrorPayload documents Cartesia's error envelope:
// {"error_code","title","message","request_id","doc_url"}.
type cartesiaErrorPayload struct {
	ErrorCode *string         `json:"error_code"`
	Title     *string         `json:"title"`
	Message   *string         `json:"message"`
	RequestID *string         `json:"request_id"`
	DocURL    json.RawMessage `json:"doc_url"`
}

// handleError converts an HTTP error into a ProviderError, parsing
// Cartesia's error envelope when present. Matches the TypeScript SDK's
// `${title}: ${message}` message format.
func handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload cartesiaErrorPayload
		// TS cartesiaErrorDataSchema: title, message and request_id are all
		// required strings; a body that does not match gives the HTTP status
		// text.
		message := http.StatusText(statusErr.StatusCode)
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil &&
			payload.Title != nil && payload.Message != nil && payload.RequestID != nil && docURLMatchesSchema(payload.DocURL) {
			message = *payload.Title + ": " + *payload.Message
		} else {
			payload = cartesiaErrorPayload{}
		}
		errorCode := ""
		if payload.ErrorCode != nil {
			errorCode = *payload.ErrorCode
		}
		providerErr := providererrors.NewProviderError("cartesia", statusErr.StatusCode, errorCode, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		return providerErr
	}
	return providererrors.NewProviderError("cartesia", 0, "", err.Error(), err)
}

// docURLMatchesSchema is `doc_url: z.string().optional()`: absent or a string;
// an explicit null (or any other type) fails the schema.
func docURLMatchesSchema(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var s string
	return json.Unmarshal(raw, &s) == nil
}
