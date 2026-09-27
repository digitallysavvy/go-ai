package groq

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// groqErrorPayload mirrors TS groqErrorDataSchema:
// {"error":{"message","type"}} — groqFailedResponseHandler decodes this and
// uses error.message as the surfaced error text.
type groqErrorPayload struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type,omitempty"`
	} `json:"error"`
}

// parseGroqProviderError unwraps an HTTPStatusError and, when the body
// matches Groq's {"error":{"message","type"}} envelope, returns a
// ProviderError carrying error.message and error.type as the code, instead
// of a raw "HTTP <code>: <body>" message.
func parseGroqProviderError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil
	}
	var payload groqErrorPayload
	if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr != nil || payload.Error.Message == "" {
		return nil
	}
	return providererrors.NewProviderError("groq", statusErr.StatusCode, payload.Error.Type, payload.Error.Message, err)
}
