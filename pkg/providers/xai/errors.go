package xai

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// newXAIProviderError is TS xaiFailedResponseHandler
// (createJsonErrorResponseHandler over xaiErrorDataSchema): the message
// follows the matching union member, or is the status text when the body
// matches none; the error keeps the status, response headers, body and
// parsed data (TS APICallError), so retries honor retry-after headers.
func newXAIProviderError(provider string, statusCode int, headers http.Header, body []byte) *providererrors.ProviderError {
	body = []byte(textdecode.UTF8(body)) // TS response.text()
	message, matched := xaiErrorMessage(body)
	if !matched {
		message = http.StatusText(statusCode)
	}
	errorCode := ""
	var responsesError struct {
		Code  *string `json:"code"`
		Error *string `json:"error"`
	}
	var parsed struct {
		Error struct {
			Type string      `json:"type"`
			Code interface{} `json:"code"`
		} `json:"error"`
	}
	if securejson.Unmarshal(body, &responsesError) == nil && responsesError.Code != nil && responsesError.Error != nil {
		errorCode = *responsesError.Code
	} else if securejson.Unmarshal(body, &parsed) == nil {
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
	cause := &internalhttp.HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}
	pe := providererrors.NewProviderErrorFromResponse(provider, 0, errorCode, message, cause)
	// TS APICallError.responseHeaders keys are lower case.
	_, pe.ResponseHeaders, _ = cause.HTTPStatusDetails()
	return pe
}

// xaiStatusError converts an HTTP status error of an xAI API call with
// newXAIProviderError; ok is false for any other error.
func xaiStatusError(provider string, err error) (*providererrors.ProviderError, bool) {
	var statusErr *internalhttp.HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode < 400 {
		return nil, false
	}
	return newXAIProviderError(provider, statusErr.StatusCode, statusErr.Headers, statusErr.Body), true
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
//
// codePresent reports a present, non-null code: TS `code ?? undefined` keeps
// an empty string.
func newXAIResponsesStreamError(providerName, message, code string, codePresent bool, eventType string, data interface{}) *providererrors.StreamProviderError {
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
	if codePresent || code != "" {
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

// xaiErrorMessage extracts the message of a raw xAI error response body,
// mirroring the TS SDK's xaiFailedResponseHandler / xaiErrorDataSchema
// union. It tries, in order:
//  1. The structured API error shape: {"error": {"message": "..."}}.
//  2. The Responses API shape: {"code": "...", "error": "..."} -> "code: error".
//  3. The plain speech error shape: {"error": "some string"}.
//
// matched is false when none of these shapes match.
func xaiErrorMessage(body []byte) (message string, matched bool) {
	body = []byte(textdecode.UTF8(body)) // TS response.text()
	// 1. Structured API error shape (object under "error").
	// Each member's strings are required (z.string()), so an empty string
	// matches and is the message.
	var apiErr struct {
		Error *struct {
			Message *string `json:"message"`
			Type    *string `json:"type"`
			// code: z.union([z.string(), z.number()]).nullish()
			Code interface{} `json:"code"`
		} `json:"error"`
	}
	if err := securejson.Unmarshal(body, &apiErr); err == nil && apiErr.Error != nil && apiErr.Error.Message != nil && xaiStringOrNumber(apiErr.Error.Code) {
		return *apiErr.Error.Message, true
	}

	// 2. Responses API shape: {"code": "...", "error": "..."}.
	var responsesErr struct {
		Code  *string `json:"code"`
		Error *string `json:"error"`
	}
	if err := securejson.Unmarshal(body, &responsesErr); err == nil &&
		responsesErr.Code != nil && responsesErr.Error != nil {
		return *responsesErr.Code + ": " + *responsesErr.Error, true
	}

	// 3. Plain speech error shape: {"error": "some string"}.
	var speechErr struct {
		Error *string `json:"error"`
	}
	if err := securejson.Unmarshal(body, &speechErr); err == nil && speechErr.Error != nil {
		return *speechErr.Error, true
	}

	return "", false
}

// xaiStringOrNumber reports whether a decoded value is absent, null, a
// string or a number.
func xaiStringOrNumber(v interface{}) bool {
	switch v.(type) {
	case nil, string, float64:
		return true
	default:
		return false
	}
}

// xaiWireValuePresent reports whether the decoded JSON value holds a non-null
// value at path (object keys).
func xaiWireValuePresent(data interface{}, path ...string) bool {
	for _, key := range path {
		m, ok := data.(map[string]interface{})
		if !ok {
			return false
		}
		data, ok = m[key]
		if !ok {
			return false
		}
	}
	return data != nil
}
