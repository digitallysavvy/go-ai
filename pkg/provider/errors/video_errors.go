package errors

import (
	"fmt"
)

// NoVideoGeneratedError indicates no videos were generated
type NoVideoGeneratedError struct {
	Message string
}

// Error implements the error interface
func (e *NoVideoGeneratedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "no videos were generated"
}

// NewNoVideoGeneratedError creates a new NoVideoGeneratedError
func NewNoVideoGeneratedError() *NoVideoGeneratedError {
	return &NoVideoGeneratedError{
		Message: "no videos were generated",
	}
}

// NewNoVideoGeneratedErrorWithMessage creates a new NoVideoGeneratedError with a custom message
func NewNoVideoGeneratedErrorWithMessage(message string) *NoVideoGeneratedError {
	return &NoVideoGeneratedError{
		Message: message,
	}
}

// VideoGenerationError represents an error during video generation
type VideoGenerationError struct {
	Provider string
	ModelID  string
	Message  string
	Cause    error
}

// Error implements the error interface
func (e *VideoGenerationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("video generation failed [provider=%s, model=%s]: %s: %v", e.Provider, e.ModelID, e.Message, e.Cause)
	}
	return fmt.Sprintf("video generation failed [provider=%s, model=%s]: %s", e.Provider, e.ModelID, e.Message)
}

// Unwrap returns the underlying cause
func (e *VideoGenerationError) Unwrap() error {
	return e.Cause
}

// NewVideoGenerationError creates a new VideoGenerationError
func NewVideoGenerationError(provider, modelID, message string, cause error) *VideoGenerationError {
	return &VideoGenerationError{
		Provider: provider,
		ModelID:  modelID,
		Message:  message,
		Cause:    cause,
	}
}

// VideoPollingTimeoutError indicates a polling timeout
type VideoPollingTimeoutError struct {
	Provider string
	ModelID  string
	JobID    string
	Timeout  string
}

// Error implements the error interface
func (e *VideoPollingTimeoutError) Error() string {
	return fmt.Sprintf("video generation polling timeout [provider=%s, model=%s, job=%s, timeout=%s]", e.Provider, e.ModelID, e.JobID, e.Timeout)
}

// NewVideoPollingTimeoutError creates a new VideoPollingTimeoutError
func NewVideoPollingTimeoutError(provider, modelID, jobID, timeout string) *VideoPollingTimeoutError {
	return &VideoPollingTimeoutError{
		Provider: provider,
		ModelID:  modelID,
		JobID:    jobID,
		Timeout:  timeout,
	}
}
