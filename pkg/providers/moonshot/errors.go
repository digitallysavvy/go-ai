package moonshot

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// moonshotErrorEnvelope documents the Moonshot API error shape:
// {"error": {"message": string, "type": string|null, "code": string|null}}.
// Mirrors TS moonshotAIErrorSchema.
type moonshotErrorEnvelope struct {
	Error moonshotErrorPayload `json:"error"`
}

type moonshotErrorPayload struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

// parseMoonshotProviderError extracts a Moonshot {error:{message,type,code}}
// envelope from an HTTP error response (via internalhttp.HTTPStatusError) and
// returns a ProviderError carrying the provider's error code. If the body
// doesn't match the expected shape, err is returned unwrapped so a generic
// error is still produced upstream.
func parseMoonshotProviderError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return err
	}

	var envelope moonshotErrorEnvelope
	var raw map[string]interface{}
	if unmarshalErr := json.Unmarshal(statusErr.Body, &envelope); unmarshalErr != nil || envelope.Error.Message == "" {
		return err
	}
	_ = json.Unmarshal(statusErr.Body, &raw)

	code := envelope.Error.Code
	if code == "" {
		code = envelope.Error.Type
	}

	return &providererrors.ProviderError{
		Provider:     "moonshot",
		StatusCode:   statusErr.StatusCode,
		ErrorCode:    code,
		Message:      envelope.Error.Message,
		Cause:        err,
		ResponseBody: string(statusErr.Body),
		Data:         raw,
	}
}

// moonshotStreamErrorStatusCode mirrors TS getMoonshotAIStreamErrorMetadata:
// it classifies a Moonshot error `type` string into an HTTP-like status code
// so ProviderError.IsRetryable() reports the same retry behavior TS assigns
// (rate limits and transient server errors are retryable).
func moonshotStreamErrorStatusCode(errType string) int {
	switch errType {
	case "rate_limit_exceeded", "rate_limit_error":
		return 429
	case "server_error", "api_error", "internal_server_error":
		return 500
	case "overloaded_error", "service_unavailable":
		return 503
	case "timeout", "timeout_error":
		return 504
	case "authentication_error", "invalid_api_key":
		return 401
	case "permission_error":
		return 403
	case "not_found_error", "model_not_found":
		return 404
	case "bad_request", "context_length_exceeded", "invalid_request_error":
		return 400
	default:
		return 500
	}
}

// newMoonshotStreamProviderError builds the ProviderError surfaced for an
// {"error": {...}} envelope encountered mid-stream (a Moonshot Chat
// Completions SSE error frame). Mirrors TS createMoonshotAIStreamError.
func newMoonshotStreamProviderError(payload moonshotErrorPayload, raw json.RawMessage) error {
	code := payload.Code
	if code == "" {
		code = payload.Type
	}
	var data interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	return &providererrors.ProviderError{
		Provider:     "moonshot",
		StatusCode:   moonshotStreamErrorStatusCode(payload.Type),
		ErrorCode:    code,
		Message:      payload.Message,
		ResponseBody: string(raw),
		Data:         data,
	}
}
