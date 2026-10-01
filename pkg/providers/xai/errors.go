package xai

import (
	"encoding/json"
	"fmt"
	"strconv"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func newXAIProviderError(provider string, statusCode int, body []byte) *providererrors.ProviderError {
	var responsesError struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &responsesError); err == nil && responsesError.Code != "" && responsesError.Error != "" {
		return providererrors.NewProviderError(provider, statusCode, responsesError.Code, responsesError.Code+": "+responsesError.Error, nil)
	}

	var parsed struct {
		Error struct {
			Message string      `json:"message"`
			Type    string      `json:"type"`
			Code    interface{} `json:"code"`
		} `json:"error"`
	}

	message := string(body)
	errorCode := ""
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Error.Message != "" {
			message = parsed.Error.Message
		}
		switch code := parsed.Error.Code.(type) {
		case string:
			errorCode = code
		case float64:
			errorCode = fmt.Sprintf("%.0f", code)
		}
		if errorCode == "" {
			errorCode = parsed.Error.Type
		}
	}

	return providererrors.NewProviderError(provider, statusCode, errorCode, message, nil)
}

func formatXAIResponseError(code string, message string) string {
	if code != "" && message != "" {
		return code + ": " + message
	}
	return message
}

// ModerationError is returned by the xAI video model when the API rejects
// generated content due to content moderation policy.
type ModerationError struct {
	Code    string
	Message string
}

// Error implements the error interface.
func (e *ModerationError) Error() string {
	if e.Code != "" {
		return "xai: moderation rejection [" + e.Code + "]: " + e.Message
	}
	return "xai: moderation rejection: " + e.Message
}

// XAIStreamError represents a terminal stream "error" event from xAI Responses.
type XAIStreamError struct {
	Code    string
	Message string
}

func (e *XAIStreamError) Error() string {
	if e.Code != "" {
		return "xai.responses stream error [" + e.Code + "]: " + e.Message
	}
	return "xai.responses stream error: " + e.Message
}

// XAIStreamIncomplete represents a response.incomplete SSE event.
type XAIStreamIncomplete struct {
	Reason string
}

// xaiStreamErrorCodeMetadata mirrors TS xai-responses-language-model.ts's
// getXaiResponsesStreamErrorMetadata: known xAI error codes map to a status
// code + retryability when the code isn't itself already a numeric HTTP
// status.
func xaiStreamErrorCodeMetadata(code string) (statusCode int, isRetryable bool, ok bool) {
	switch code {
	case "rate_limit_exceeded", "rate_limit_error":
		return 429, true, true
	case "insufficient_quota":
		return 429, false, true
	case "api_error", "internal_server_error", "server_error":
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

// xaiStreamErrorHTTPStatusCode mirrors TS getHttpStatusCode: a numeric code
// (or a 3-digit numeric string) between 400 and 599 is treated as an
// explicit HTTP status code carried directly in the error's "code" field.
func xaiStreamErrorHTTPStatusCode(code string) (int, bool) {
	if len(code) != 3 {
		return 0, false
	}
	n, err := strconv.Atoi(code)
	if err != nil {
		return 0, false
	}
	if n < 400 || n > 599 {
		return 0, false
	}
	return n, true
}

func xaiStreamErrorIsRetryableStatusCode(statusCode int) bool {
	return statusCode == 408 || statusCode == 409 || statusCode == 429 || statusCode >= 500
}

// newXAIResponsesStreamError builds a *providererrors.StreamProviderError
// from a mid-stream xAI Responses error, mirroring TS
// createXaiResponsesStreamError. eventType is "error" or "response.failed"
// (used as the error's Type, matching TS's `type: eventType`). data is the
// raw event payload, attached verbatim for callers that inspect it further.
func newXAIResponsesStreamError(providerName, message, code, eventType string, data interface{}) *providererrors.StreamProviderError {
	var statusCode *int
	var isRetryable *bool

	if sc, ok := xaiStreamErrorHTTPStatusCode(code); ok {
		statusCode = &sc
		r := xaiStreamErrorIsRetryableStatusCode(sc)
		isRetryable = &r
	} else if sc, retryable, matched := xaiStreamErrorCodeMetadata(code); matched {
		statusCode = &sc
		isRetryable = &retryable
	}

	var codeArg interface{}
	if code != "" {
		codeArg = code
	}
	return providererrors.NewStreamProviderError(message, providerName, eventType, codeArg, statusCode, isRetryable, data)
}

func (e *XAIStreamIncomplete) Error() string {
	if e.Reason != "" {
		return "xai.responses stream incomplete: " + e.Reason
	}
	return "xai.responses stream incomplete"
}

// XAIStreamFailed represents a response.failed SSE event.
type XAIStreamFailed struct {
	Reason  string
	Code    string
	Message string
}

func (e *XAIStreamFailed) Error() string {
	if e.Message != "" {
		return "xai.responses stream failed: " + e.Message
	}
	if e.Reason != "" {
		return "xai.responses stream failed: " + e.Reason
	}
	if e.Code != "" {
		return "xai.responses stream failed: " + e.Code
	}
	return "xai.responses stream failed"
}

// xaiAPIErrorBody is the structured `{"error": {"message": ..., "code": ...}}`
// error shape used by most xAI REST endpoints (chat completions, images,
// videos).
type xaiAPIErrorBody struct {
	Error struct {
		Message string      `json:"message"`
		Type    string      `json:"type,omitempty"`
		Code    interface{} `json:"code,omitempty"`
	} `json:"error"`
}

// xaiResponsesErrorBody is the `{"code": ..., "error": "..."}` shape used by
// the xAI Responses API.
type xaiResponsesErrorBody struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

// xaiSpeechErrorBody is the simpler `{"error": "some string"}` shape used by
// the xAI text-to-speech endpoint, e.g. {"error":"speed must be between 0.7
// and 1.5"}.
type xaiSpeechErrorBody struct {
	Error string `json:"error"`
}

// parseXAIErrorMessage extracts a human-readable error message from a raw
// xAI error response body, mirroring the TS SDK's xaiFailedResponseHandler /
// xaiErrorDataSchema union. It tries, in order:
//  1. The structured API error shape: {"error": {"message": "..."}}.
//  2. The Responses API shape: {"code": "...", "error": "..."} -> "code: error".
//  3. The plain speech error shape: {"error": "some string"}.
//
// If none of these shapes match, the raw body (trimmed) is returned as-is.
func parseXAIErrorMessage(body []byte) string {
	// 1. Structured API error shape (object under "error").
	var apiErr xaiAPIErrorBody
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return apiErr.Error.Message
	}

	// 2. Responses API shape: {"code": "...", "error": "..."}.
	var responsesErr xaiResponsesErrorBody
	if err := json.Unmarshal(body, &responsesErr); err == nil &&
		responsesErr.Code != "" && responsesErr.Error != "" {
		return responsesErr.Code + ": " + responsesErr.Error
	}

	// 3. Plain speech error shape: {"error": "some string"}.
	var speechErr xaiSpeechErrorBody
	if err := json.Unmarshal(body, &speechErr); err == nil && speechErr.Error != "" {
		return speechErr.Error
	}

	return string(body)
}
