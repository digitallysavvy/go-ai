package errors

import (
	"errors"
	"fmt"
)

// Error types for the AI SDK
var (
	// ErrInvalidInput indicates invalid input parameters
	ErrInvalidInput = errors.New("invalid input")

	// ErrModelNotFound indicates the requested model was not found
	ErrModelNotFound = errors.New("model not found")

	// ErrProviderNotFound indicates the requested provider was not found
	ErrProviderNotFound = errors.New("provider not found")

	// ErrToolNotFound indicates the requested tool was not found
	ErrToolNotFound = errors.New("tool not found")

	// ErrValidationFailed indicates schema validation failed
	ErrValidationFailed = errors.New("validation failed")

	// ErrUnsupportedFeature indicates a feature is not supported by the provider
	ErrUnsupportedFeature = errors.New("unsupported feature")
)

// ProviderError represents an error from an AI provider
type ProviderError struct {
	// Provider name
	Provider string

	// HTTP status code (if applicable)
	StatusCode int

	// Provider-specific error code
	ErrorCode string

	// Error message
	Message string

	// Underlying cause
	Cause error
}

// Error implements the error interface
func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s provider error (%d): %s - %s (caused by: %v)",
			e.Provider, e.StatusCode, e.ErrorCode, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s provider error (%d): %s - %s",
		e.Provider, e.StatusCode, e.ErrorCode, e.Message)
}

// Unwrap returns the underlying cause
func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// IsProviderError checks if an error is a ProviderError
func IsProviderError(err error) bool {
	var providerErr *ProviderError
	return errors.As(err, &providerErr)
}

// NewProviderError creates a new provider error
func NewProviderError(provider string, statusCode int, errorCode, message string, cause error) *ProviderError {
	return &ProviderError{
		Provider:   provider,
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Message:    message,
		Cause:      cause,
	}
}

// ValidationError represents a validation error
type ValidationError struct {
	// Field that failed validation
	Field string

	// Error message
	Message string

	// Underlying cause
	Cause error
}

// Error implements the error interface
func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

// Unwrap returns the underlying cause
func (e *ValidationError) Unwrap() error {
	return e.Cause
}

// IsValidationError checks if an error is a ValidationError
func IsValidationError(err error) bool {
	var validationErr *ValidationError
	return errors.As(err, &validationErr)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string, cause error) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
		Cause:   cause,
	}
}

// ToolExecutionError represents an error during tool execution
type ToolExecutionError struct {
	// Name of the tool that failed
	ToolName string

	// Tool call ID
	ToolCallID string

	// Error message
	Message string

	// Underlying cause
	Cause error
}

// Error implements the error interface
func (e *ToolExecutionError) Error() string {
	if e.ToolCallID != "" {
		return fmt.Sprintf("tool execution error for '%s' (call ID: %s): %s",
			e.ToolName, e.ToolCallID, e.Message)
	}
	return fmt.Sprintf("tool execution error for '%s': %s", e.ToolName, e.Message)
}

// Unwrap returns the underlying cause
func (e *ToolExecutionError) Unwrap() error {
	return e.Cause
}

// IsToolExecutionError checks if an error is a ToolExecutionError
func IsToolExecutionError(err error) bool {
	var toolErr *ToolExecutionError
	return errors.As(err, &toolErr)
}

// NewToolExecutionError creates a new tool execution error
func NewToolExecutionError(toolName, toolCallID, message string, cause error) *ToolExecutionError {
	return &ToolExecutionError{
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Message:    message,
		Cause:      cause,
	}
}

// StreamError represents an error during streaming
type StreamError struct {
	// Error message
	Message string

	// Underlying cause
	Cause error
}

// Error implements the error interface
func (e *StreamError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("stream error: %s (caused by: %v)", e.Message, e.Cause)
	}
	return fmt.Sprintf("stream error: %s", e.Message)
}

// Unwrap returns the underlying cause
func (e *StreamError) Unwrap() error {
	return e.Cause
}

// IsStreamError checks if an error is a StreamError
func IsStreamError(err error) bool {
	var streamErr *StreamError
	return errors.As(err, &streamErr)
}

// NewStreamError creates a new stream error
func NewStreamError(message string, cause error) *StreamError {
	return &StreamError{
		Message: message,
		Cause:   cause,
	}
}

// RateLimitError represents a rate limit error from a provider
type RateLimitError struct {
	// Provider name
	Provider string

	// Retry after duration in seconds (if provided by the provider)
	RetryAfterSeconds *int

	// Error message
	Message string

	// Underlying cause
	Cause error
}

// Error implements the error interface
func (e *RateLimitError) Error() string {
	if e.RetryAfterSeconds != nil {
		return fmt.Sprintf("rate limit exceeded for %s (retry after %d seconds): %s",
			e.Provider, *e.RetryAfterSeconds, e.Message)
	}
	return fmt.Sprintf("rate limit exceeded for %s: %s", e.Provider, e.Message)
}

// Unwrap returns the underlying cause
func (e *RateLimitError) Unwrap() error {
	return e.Cause
}

// IsRateLimitError checks if an error is a RateLimitError
func IsRateLimitError(err error) bool {
	var rateLimitErr *RateLimitError
	return errors.As(err, &rateLimitErr)
}

// NewRateLimitError creates a new rate limit error
func NewRateLimitError(provider, message string, retryAfter *int, cause error) *RateLimitError {
	return &RateLimitError{
		Provider:          provider,
		RetryAfterSeconds: retryAfter,
		Message:           message,
		Cause:             cause,
	}
}
