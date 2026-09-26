package fireworks

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// fireworksErrorPayload documents the object error envelope Fireworks
// actually returns: {"error":{"object","type","code","message"}}. A bare
// string error message is not this shape and falls back to the generic
// handling in handleError.
type fireworksErrorPayload struct {
	Error struct {
		Object  string `json:"object,omitempty"`
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseFireworksProviderError unwraps an HTTPStatusError and, when the body
// matches Fireworks' object error envelope, returns a ProviderError carrying
// error.message and error.code instead of the raw response body text.
func parseFireworksProviderError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}
	var payload fireworksErrorPayload
	if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr != nil || payload.Error.Message == "" {
		return nil
	}
	return providererrors.NewProviderError("fireworks", statusErr.StatusCode, payload.Error.Code, payload.Error.Message, err)
}
