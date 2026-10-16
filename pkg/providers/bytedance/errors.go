package bytedance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Error represents a ByteDance API error
type Error struct {
	Code    int
	Message string
	Details string

	// httpStatus and httpBody carry the non-2xx HTTP response this error was
	// built from so retry classification follows the TS APICallError rule
	// (only 408, 409, 429 and 5xx are retryable). Zero when the error is not
	// an HTTP response.
	httpStatus int
	httpBody   []byte
}

// HTTPStatusDetails implements providererrors.HTTPStatusDetails.
func (e *Error) HTTPStatusDetails() (int, map[string]string, []byte) {
	return e.httpStatus, nil, e.httpBody
}

// newHTTPError builds the error for a non-2xx response, following TS
// byteDanceFailedResponseHandler: the message is
// `error.message ?? message ?? 'Unknown error'` of a body that matches the
// error schema, and the HTTP status text for any other body.
func newHTTPError(statusCode int, body []byte) *Error {
	return &Error{Code: statusCode, Message: byteDanceErrorMessage(statusCode, body), httpStatus: statusCode, httpBody: body}
}

func byteDanceErrorMessage(statusCode int, body []byte) string {
	var raw struct {
		Error   json.RawMessage `json:"error"`
		Message json.RawMessage `json:"message"`
	}
	statusText := http.StatusText(statusCode)
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(body, &raw) != nil {
		return statusText
	}
	var nested, top *string
	if isJSONPresent(raw.Error) {
		var e struct {
			Message *string         `json:"message"`
			Code    json.RawMessage `json:"code"`
		}
		if json.Unmarshal(raw.Error, &e) != nil || e.Message == nil {
			return statusText
		}
		if isJSONPresent(e.Code) {
			var code string
			if json.Unmarshal(e.Code, &code) != nil {
				return statusText
			}
		}
		nested = e.Message
	}
	if isJSONPresent(raw.Message) {
		var m string
		if json.Unmarshal(raw.Message, &m) != nil {
			return statusText
		}
		top = &m
	}
	switch {
	case nested != nil:
		return *nested
	case top != nil:
		return *top
	}
	return "Unknown error"
}

// isJSONPresent reports whether a raw JSON member is present and not null.
func isJSONPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null"
}

// Error implements the error interface
func (e *Error) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("bytedance error (code %d): %s - %s", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("bytedance error (code %d): %s", e.Code, e.Message)
}

// NewError creates a new ByteDance error
func NewError(code int, message, details string) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Details: details,
	}
}

// NewVideoGenerationError creates a video generation error
func NewVideoGenerationError(details string) *Error {
	return &Error{
		Code:    500,
		Message: "Video generation failed",
		Details: details,
	}
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(duration string) *Error {
	return &Error{
		Code:    504,
		Message: "Video generation timed out",
		Details: fmt.Sprintf("Generation timed out after %s", duration),
	}
}
