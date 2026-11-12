package bytedance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	httpStatus  int
	httpBody    []byte
	httpHeaders map[string]string

	// textOnly makes Error() the bare Message: the TS AISDKError
	// BYTEDANCE_VIDEO_GENERATION_ERROR text, with no Go prefix.
	textOnly bool
}

// HTTPStatusDetails implements providererrors.HTTPStatusDetails.
func (e *Error) HTTPStatusDetails() (int, map[string]string, []byte) {
	return e.httpStatus, e.httpHeaders, e.httpBody
}

// Unwrap exposes an HTTP response error as the *providererrors.ProviderError
// TS reports (APICallError with statusCode, responseHeaders, responseBody,
// data), so the retry delay honors retry-after-ms / retry-after like TS
// (pkg/internal/retry reads ProviderError.ResponseHeaders) and callers can
// match it with errors.As. nil for errors that are not an HTTP response.
func (e *Error) Unwrap() error {
	if e == nil || e.httpStatus == 0 {
		return nil
	}
	pe := &providererrors.ProviderError{
		Provider:        "bytedance",
		StatusCode:      e.httpStatus,
		Message:         e.Message,
		ResponseHeaders: e.httpHeaders,
		ResponseBody:    string(e.httpBody),
	}
	pe.Data = byteDanceErrorData(e.httpBody)
	return pe
}

// byteDanceErrorData is the zod output of byteDanceErrorSchema ({error?:
// {message, code?}, message?}, undeclared keys stripped, explicit nulls
// kept) for a matching body, else nil (TS APICallError.data is undefined
// for an off-schema body).
func byteDanceErrorData(body []byte) interface{} {
	if _, matched := byteDanceErrorParse(0, body); !matched {
		return nil
	}
	var obj map[string]interface{}
	if securejson.Unmarshal(body, &obj) != nil {
		return nil
	}
	data := map[string]interface{}{}
	if v, ok := obj["error"]; ok {
		if inner, isObj := v.(map[string]interface{}); isObj {
			e := map[string]interface{}{"message": inner["message"]}
			if code, has := inner["code"]; has {
				e["code"] = code
			}
			data["error"] = e
		} else {
			data["error"] = nil
		}
	}
	if v, ok := obj["message"]; ok {
		data["message"] = v
	}
	return data
}

// newHTTPError builds the error for a non-2xx response, following TS
// byteDanceFailedResponseHandler: the message is
// `error.message ?? message ?? 'Unknown error'` of a body that matches the
// error schema, and the HTTP status text for any other body.
//
// Error() is the TS APICallError message alone (textOnly), with no Go
// prefix (bytedance-video-model.ts:507).
func newHTTPError(statusCode int, headers http.Header, body []byte) *Error {
	return &Error{Code: statusCode, Message: byteDanceErrorMessage(statusCode, body), httpStatus: statusCode, httpBody: body, httpHeaders: providerutils.ExtractHeaders(headers), textOnly: true}
}

func byteDanceErrorMessage(statusCode int, body []byte) string {
	message, _ := byteDanceErrorParse(statusCode, body)
	return message
}

// byteDanceErrorParse is byteDanceErrorMessage plus whether the body matched
// byteDanceErrorSchema.
func byteDanceErrorParse(statusCode int, body []byte) (string, bool) {
	var raw struct {
		Error   json.RawMessage `json:"error"`
		Message json.RawMessage `json:"message"`
	}
	statusText := http.StatusText(statusCode)
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || trimmed[0] != '{' || securejson.Unmarshal(body, &raw) != nil {
		return statusText, false
	}
	var nested, top *string
	if isJSONPresent(raw.Error) {
		var e struct {
			Message *string         `json:"message"`
			Code    json.RawMessage `json:"code"`
		}
		if json.Unmarshal(raw.Error, &e) != nil || e.Message == nil {
			return statusText, false
		}
		if isJSONPresent(e.Code) {
			var code string
			if json.Unmarshal(e.Code, &code) != nil {
				return statusText, false
			}
		}
		nested = e.Message
	}
	if isJSONPresent(raw.Message) {
		var m string
		if json.Unmarshal(raw.Message, &m) != nil {
			return statusText, false
		}
		top = &m
	}
	switch {
	case nested != nil:
		return *nested, true
	case top != nil:
		return *top, true
	}
	return "Unknown error", true
}

// isJSONPresent reports whether a raw JSON member is present and not null.
func isJSONPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null"
}

// Error implements the error interface
func (e *Error) Error() string {
	if e.textOnly {
		return e.Message
	}
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

// newVideoGenerationErrorText is a video generation error whose whole text
// is the TS AISDKError message (BYTEDANCE_VIDEO_GENERATION_ERROR thrown by
// doStart / doStatus); it is still a *Error with Code 500.
func newVideoGenerationErrorText(message string) *Error {
	return &Error{Code: 500, Message: message, textOnly: true}
}
