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

// groqStreamErrorMetadata returns the inferred (statusCode, isRetryable)
// pair for a mid-stream Groq error's type field, mirroring TS
// groq-chat-language-model.ts's getGroqStreamErrorMetadata. The zero value
// (0, false) means "no inference" (TS returns {}).
func groqStreamErrorMetadata(errType string) (statusCode int, isRetryable bool) {
	switch errType {
	case "rate_limit_error":
		return 429, true
	case "api_error", "internal_server_error", "server_error":
		return 500, true
	case "overloaded_error", "service_unavailable":
		return 503, true
	case "timeout", "timeout_error":
		return 504, true
	case "authentication_error", "invalid_api_key":
		return 401, false
	case "permission_error":
		return 403, false
	case "not_found_error", "model_not_found":
		return 404, false
	case "bad_request", "context_length_exceeded", "invalid_request_error":
		return 400, false
	default:
		return 0, false
	}
}

// newGroqStreamProviderErrorChunk builds a *providererrors.StreamProviderError
// from a mid-stream `error` frame's raw {"message","type"} payload, mirroring
// TS createGroqStreamError(value.error, value). fullFrame is the whole SSE
// chunk (e.g. `{"error":{"message","type"}}`), used verbatim for `data` to
// match TS's `data: value` (the full chunk, not just `value.error`); it may
// be nil/empty, in which case raw is used as a fallback.
func newGroqStreamProviderErrorChunk(raw json.RawMessage, fullFrame json.RawMessage) *providererrors.StreamProviderError {
	var payload struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	_ = json.Unmarshal(raw, &payload)
	message := payload.Message
	if message == "" {
		message = groqStreamErrorText(raw)
	}
	statusCode, isRetryable := groqStreamErrorMetadata(payload.Type)
	dataSrc := fullFrame
	if len(dataSrc) == 0 {
		dataSrc = raw
	}
	var data interface{}
	_ = json.Unmarshal(dataSrc, &data)
	// A zero statusCode means the type wasn't recognized (TS's
	// getGroqStreamErrorMetadata returns {}); leave both statusCode and
	// isRetryable unset so NewStreamProviderError falls back to its own
	// message/status-code inference, instead of forcing isRetryable=false.
	var statusPtr *int
	var retryablePtr *bool
	if statusCode != 0 {
		statusPtr = &statusCode
		retryablePtr = &isRetryable
	}
	// TS createGroqStreamError only forwards {message, type}; Groq's error
	// shape has no separate `code` field, so code is left nil (matching TS's
	// `code: undefined`, unlike OpenAI/DeepSeek/Moonshot).
	return providererrors.NewStreamProviderError(message, "groq", payload.Type, nil, statusPtr, retryablePtr, data)
}
