package minimax

import "fmt"

// Error represents a MiniMax video generation error, matching the TS
// AISDKError name/message pairs used by MiniMaxVideoModel.
type Error struct {
	// Name is the AISDKError name (e.g. "MINIMAX_VIDEO_GENERATION_ERROR").
	Name    string
	Message string
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.Message
}

// NewVideoGenerationError creates a MINIMAX_VIDEO_GENERATION_ERROR error for
// a malformed or missing response (e.g. no task_id, no video URL).
func NewVideoGenerationError(message string) *Error {
	return &Error{Name: "MINIMAX_VIDEO_GENERATION_ERROR", Message: message}
}

// NewVideoGenerationFailedError creates a MINIMAX_VIDEO_GENERATION_FAILED
// error for a task that explicitly failed.
func NewVideoGenerationFailedError(message string) *Error {
	return &Error{Name: "MINIMAX_VIDEO_GENERATION_FAILED", Message: message}
}

// NewVideoGenerationCancelledError creates a
// MINIMAX_VIDEO_GENERATION_CANCELLED error for a cancelled task.
func NewVideoGenerationCancelledError(message string) *Error {
	return &Error{Name: "MINIMAX_VIDEO_GENERATION_CANCELLED", Message: message}
}

// NewVideoGenerationExpiredError creates a MINIMAX_VIDEO_GENERATION_EXPIRED
// error for an expired task.
func NewVideoGenerationExpiredError(message string) *Error {
	return &Error{Name: "MINIMAX_VIDEO_GENERATION_EXPIRED", Message: message}
}

// NewTimeoutError creates a MINIMAX_VIDEO_GENERATION_TIMEOUT error.
func NewTimeoutError(message string) *Error {
	return &Error{Name: "MINIMAX_VIDEO_GENERATION_TIMEOUT", Message: message}
}

// NewAPIError creates an error for a non-2xx MiniMax API response, carrying
// the parsed {error:{message}} envelope when available.
func NewAPIError(statusCode int, message string) *Error {
	return &Error{Name: "MINIMAX_API_ERROR", Message: fmt.Sprintf("MiniMax API error (status %d): %s", statusCode, message)}
}
