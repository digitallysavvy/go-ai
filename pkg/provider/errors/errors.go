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

// ValidationContext provides detailed context for validation errors
type ValidationContext struct {
	// Field path in dot notation (e.g., "message.parts[3].data")
	Field string

	// Entity name (e.g., "tool_call", "message")
	EntityName string

	// Entity identifier (e.g., "call_abc123", message ID)
	EntityID string
}

// ValidationError represents a validation error
type ValidationError struct {
	// Field that failed validation (deprecated, use Context.Field instead)
	Field string

	// Error message
	Message string

	// Underlying cause
	Cause error

	// Validation context with field path, entity name, and entity ID
	Context *ValidationContext

	// Value that failed validation (for debugging)
	Value interface{}
}

// Error implements the error interface
func (e *ValidationError) Error() string {
	var contextPrefix string

	// Use new context if available, otherwise fall back to Field for backward compatibility
	if e.Context != nil {
		contextPrefix = "Type validation failed"

		// Add field path if present
		if e.Context.Field != "" {
			contextPrefix += fmt.Sprintf(" for %s", e.Context.Field)
		}

		// Add entity information in parentheses
		if e.Context.EntityName != "" || e.Context.EntityID != "" {
			contextPrefix += " ("
			var parts []string
			if e.Context.EntityName != "" {
				parts = append(parts, e.Context.EntityName)
			}
			if e.Context.EntityID != "" {
				parts = append(parts, fmt.Sprintf("id: %q", e.Context.EntityID))
			}
			for i, part := range parts {
				if i > 0 {
					contextPrefix += ", "
				}
				contextPrefix += part
			}
			contextPrefix += ")"
		}

		contextPrefix += ":"

		// Format the full error message
		msg := contextPrefix
		if e.Value != nil {
			msg += fmt.Sprintf("\nValue: %+v.", e.Value)
		}
		if e.Message != "" {
			msg += fmt.Sprintf("\nError message: %s", e.Message)
		} else if e.Cause != nil {
			msg += fmt.Sprintf("\nError message: %v", e.Cause)
		}
		return msg
	}

	// Backward compatibility path
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

// NewValidationError creates a new validation error (backward compatible)
func NewValidationError(field, message string, cause error) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
		Cause:   cause,
	}
}

// NewValidationErrorWithContext creates a new validation error with context
func NewValidationErrorWithContext(value interface{}, message string, cause error, context *ValidationContext) *ValidationError {
	return &ValidationError{
		Value:   value,
		Message: message,
		Cause:   cause,
		Context: context,
	}
}

// WrapValidationError wraps an error with validation context
// Returns existing error if it's already a ValidationError with matching context
func WrapValidationError(value interface{}, cause error, context *ValidationContext) *ValidationError {
	// Check if it's already a ValidationError with matching context
	var existingErr *ValidationError
	if errors.As(cause, &existingErr) &&
		existingErr.Value == value &&
		existingErr.Context != nil &&
		context != nil &&
		existingErr.Context.Field == context.Field &&
		existingErr.Context.EntityName == context.EntityName &&
		existingErr.Context.EntityID == context.EntityID {
		return existingErr
	}

	// Create new ValidationError with context
	message := ""
	if existingErr != nil && existingErr.Message != "" {
		message = existingErr.Message
	}

	return &ValidationError{
		Value:   value,
		Message: message,
		Cause:   cause,
		Context: context,
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
