package fishaudio

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// fishAudioErrorPayload documents Fish Audio's error envelope for documented
// error responses (401 no permission, 402 no payment):
// https://docs.fish.audio/api-reference/endpoint/openapi-v1/text-to-speech
type fishAudioErrorPayload struct {
	Status  *int   `json:"status"`
	Message string `json:"message"`
}

// handleError converts an HTTP error into a ProviderError, parsing Fish
// Audio's `{"status","message"}` error shape when present.
func handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload fishAudioErrorPayload
		message := string(statusErr.Body)
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && payload.Message != "" {
			message = payload.Message
		} else if message == "" {
			message = "Unknown Fish Audio error"
		}
		providerErr := providererrors.NewProviderError("fish-audio", statusErr.StatusCode, "", message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		return providerErr
	}
	return providererrors.NewProviderError("fish-audio", 0, "", err.Error(), err)
}
