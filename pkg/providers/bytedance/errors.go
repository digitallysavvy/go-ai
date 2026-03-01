package bytedance

import "fmt"

// Error represents a ByteDance API error
type Error struct {
	Code    int
	Message string
	Details string
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
