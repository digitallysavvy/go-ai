package klingai

import (
	"fmt"
)

// ErrCodeKlingVideoMissingOptions is the error code returned when required motion-control
// fields (VideoUrl, CharacterOrientation, Mode) are absent.
const ErrCodeKlingVideoMissingOptions = "KLINGAI_VIDEO_MISSING_OPTIONS"

// Error represents a KlingAI API error
type Error struct {
	Code      int    // HTTP status code
	ErrorCode string // semantic error code (e.g. ErrCodeKlingVideoMissingOptions)
	Message   string
	Details   string

	// httpStatus and httpBody carry the non-2xx HTTP response this error was
	// built from, so retry classification follows the TS APICallError rule
	// (only 408, 409, 429 and 5xx are retryable) instead of retrying every
	// error. Zero for errors that are not an HTTP response.
	httpStatus int
	httpBody   []byte
}

// HTTPStatusDetails implements providererrors.HTTPStatusDetails.
func (e *Error) HTTPStatusDetails() (int, map[string]string, []byte) {
	return e.httpStatus, nil, e.httpBody
}

// Error implements the error interface
func (e *Error) Error() string {
	if e.ErrorCode != "" {
		if e.Details != "" {
			return fmt.Sprintf("klingai error (%s): %s - %s", e.ErrorCode, e.Message, e.Details)
		}
		return fmt.Sprintf("klingai error (%s): %s", e.ErrorCode, e.Message)
	}
	if e.Details != "" {
		return fmt.Sprintf("klingai error (code %d): %s - %s", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("klingai error (code %d): %s", e.Code, e.Message)
}

// NewError creates a new KlingAI error
func NewError(code int, message, details string) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Details: details,
	}
}

// Common error constructors

// NewAuthError creates an authentication error
func NewAuthError(details string) *Error {
	return &Error{
		Code:    401,
		Message: "Authentication failed",
		Details: details,
	}
}

// NewVideoGenerationError creates a video generation error.
// Sets ErrorCode to "KLINGAI_VIDEO_GENERATION_ERROR", matching TS AISDKError name.
func NewVideoGenerationError(details string) *Error {
	return &Error{
		Code:      500,
		ErrorCode: "KLINGAI_VIDEO_GENERATION_ERROR",
		Message:   "Video generation failed",
		Details:   details,
	}
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(duration string) *Error {
	return &Error{
		Code:    504,
		Message: "Video generation timeout",
		Details: fmt.Sprintf("Generation timed out after %s", duration),
	}
}

// NewInvalidOptionsError creates an invalid options error
func NewInvalidOptionsError(details string) *Error {
	return &Error{
		Code:    400,
		Message: "Invalid provider options",
		Details: details,
	}
}

// NewVideoGenerationFailedError creates an error for a task that explicitly failed
// (task_status == "failed"). Matches TS KLINGAI_VIDEO_GENERATION_FAILED.
func NewVideoGenerationFailedError(details string) *Error {
	return &Error{
		Code:      500,
		ErrorCode: "KLINGAI_VIDEO_GENERATION_FAILED",
		Message:   "Video generation failed",
		Details:   details,
	}
}

// NewMissingVideoOptionsError creates an error for missing required motion-control fields.
// Sets ErrorCode to ErrCodeKlingVideoMissingOptions.
func NewMissingVideoOptionsError(field string) *Error {
	return &Error{
		Code:      400,
		ErrorCode: ErrCodeKlingVideoMissingOptions,
		Message:   fmt.Sprintf("%s is required for motion control", field),
	}
}

// newVideoGenerationErrorText is a KLINGAI_VIDEO_GENERATION_ERROR whose whole
// text is the TS AISDKError message (no "Video generation failed" prefix).
func newVideoGenerationErrorText(message string) *Error {
	return &Error{Code: 500, ErrorCode: "KLINGAI_VIDEO_GENERATION_ERROR", Message: message}
}

// missingCredentialsError is the error DoStart and DoStatus return when no
// credentials are configured: TS throws LoadAPIKeyError, while v0.5.2 returned
// the *Error from NewAuthError. Both match with errors.As; the text is the TS
// LoadAPIKeyError message.
type missingCredentialsError struct {
	loadErr error
	auth    *Error
}

func newMissingCredentialsError(loadErr error) error {
	return &missingCredentialsError{loadErr: loadErr, auth: NewAuthError(loadErr.Error())}
}

func (e *missingCredentialsError) Error() string { return e.loadErr.Error() }

func (e *missingCredentialsError) Unwrap() []error { return []error{e.loadErr, e.auth} }
