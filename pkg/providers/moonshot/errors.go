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

// moonshotStreamErrorMetadata mirrors TS getMoonshotAIStreamErrorMetadata
// exactly, returning both statusCode and isRetryable (unlike
// moonshotStreamErrorStatusCode above, which only returns a status code and
// defaults to 500 for ProviderError.StatusCode reporting). ok is false for
// an unrecognized type (TS's `default: return {}`, both fields undefined),
// signaling the caller should not override NewStreamProviderError's own
// inference.
func moonshotStreamErrorMetadata(errType string) (statusCode int, isRetryable bool, ok bool) {
	switch errType {
	case "rate_limit_exceeded", "rate_limit_error":
		return 429, true, true
	case "server_error", "api_error", "internal_server_error":
		return 500, true, true
	case "overloaded_error", "service_unavailable":
		return 503, true, true
	case "timeout", "timeout_error":
		return 504, true, true
	case "authentication_error", "invalid_api_key":
		return 401, false, true
	case "permission_error":
		return 403, false, true
	case "not_found_error", "model_not_found":
		return 404, false, true
	case "bad_request", "context_length_exceeded", "invalid_request_error":
		return 400, false, true
	default:
		return 0, false, false
	}
}

// newMoonshotStreamProviderErrorChunk builds a
// *providererrors.StreamProviderError for attaching to StreamChunk.Err
// (P1-1c part 2), mirroring TS createMoonshotAIStreamError(value.error, value).
func newMoonshotStreamProviderErrorChunk(payload moonshotErrorPayload, raw json.RawMessage) *providererrors.StreamProviderError {
	var data interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	statusCode, isRetryable, matched := moonshotStreamErrorMetadata(payload.Type)
	var statusPtr *int
	var retryablePtr *bool
	if matched {
		statusPtr = &statusCode
		retryablePtr = &isRetryable
	}
	// TS createMoonshotAIStreamError forwards error.code as-is (no fallback
	// to type — that fallback belongs only to newMoonshotStreamProviderError
	// above, which reports a single ErrorCode string with no separate type
	// field).
	var code interface{}
	if payload.Code != "" {
		code = payload.Code
	}
	return providererrors.NewStreamProviderError(payload.Message, "moonshot", payload.Type, code, statusPtr, retryablePtr, data)
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
