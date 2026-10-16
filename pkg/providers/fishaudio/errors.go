package fishaudio

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// fishAudioErrorPayload documents Fish Audio's error envelope for documented
// error responses (401 no permission, 402 no payment):
// https://docs.fish.audio/api-reference/endpoint/openapi-v1/text-to-speech
type fishAudioErrorPayload struct {
	Status  *float64 `json:"status"`
	Message *string  `json:"message"`
}

// isJSONObject reports whether body is a JSON object (not null or an array).
func isJSONObject(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// handleError converts an HTTP error into a ProviderError, parsing Fish
// Audio's `{"status","message"}` error shape when present.
func handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload fishAudioErrorPayload
		// TS fishAudioErrorDataSchema: every field is nullish, so any JSON
		// object matches and the message is `message ?? 'Unknown Fish Audio
		// error'`; a body that is not such an object gives the status text.
		message := http.StatusText(statusErr.StatusCode)
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && isJSONObject(statusErr.Body) {
			if payload.Message != nil {
				message = *payload.Message
			} else {
				message = "Unknown Fish Audio error"
			}
		}
		return providererrors.NewProviderErrorFromResponse("fish-audio", 0, "", message, err)
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}
