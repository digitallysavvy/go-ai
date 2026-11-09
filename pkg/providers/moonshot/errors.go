package moonshot

import (
	"encoding/json"
	"errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

	// hasCode reports a present, non-null code (TS `error.code ?? undefined`
	// keeps an empty string).
	hasCode bool
}

// UnmarshalJSON decodes the payload and records whether code was present
// and not null.
func (p *moonshotErrorPayload) UnmarshalJSON(b []byte) error {
	type plain moonshotErrorPayload
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	var w struct {
		Code json.RawMessage `json:"code"`
	}
	_ = json.Unmarshal(b, &w)
	v.hasCode = len(w.Code) > 0 && string(w.Code) != "null"
	*p = moonshotErrorPayload(v)
	return nil
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
	// TS moonshotAIErrorSchema: error.message is a required string; an
	// empty one is a valid message.
	var present struct {
		Error struct {
			Message *string `json:"message"`
		} `json:"error"`
	}
	if unmarshalErr := securejson.Unmarshal(statusErr.Body, &envelope); unmarshalErr != nil || securejson.Unmarshal(statusErr.Body, &present) != nil || present.Error.Message == nil {
		return err
	}
	// TS APICallError.data: the moonshotAIErrorSchema output (undeclared
	// keys stripped).
	var raw map[string]interface{}
	_ = securejson.Unmarshal(statusErr.Body, &raw)
	data := map[string]interface{}{"error": providerutils.PickSchemaKeys(raw["error"], "message", "type", "code")}

	code := envelope.Error.Code
	if code == "" {
		code = envelope.Error.Type
	}

	// TS APICallError.responseHeaders (lower-case keys): retries honor
	// retry-after(-ms).
	_, headers, _ := statusErr.HTTPStatusDetails()
	return &providererrors.ProviderError{
		Provider:        "moonshot",
		StatusCode:      statusErr.StatusCode,
		ErrorCode:       code,
		Message:         envelope.Error.Message,
		Cause:           err,
		ResponseHeaders: headers,
		ResponseBody:    string(statusErr.Body),
		Data:            data,
	}
}

// moonshotStreamErrorMetadata mirrors TS getMoonshotAIStreamErrorMetadata
// exactly, returning both statusCode and isRetryable. ok is false for
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
	// TS createMoonshotAIStreamError forwards `error.code ?? undefined`: an
	// empty string is kept, null or absent is no code, and there is no
	// fallback to type (that fallback belongs only to the HTTP
	// parseMoonshotProviderError above, which reports a single ErrorCode
	// string with no separate type field).
	var code interface{}
	if payload.hasCode || payload.Code != "" {
		code = payload.Code
	}
	return providererrors.NewStreamProviderError(payload.Message, "moonshot", payload.Type, code, statusPtr, retryablePtr, data)
}
