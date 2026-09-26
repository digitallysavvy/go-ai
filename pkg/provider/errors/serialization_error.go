package errors

import "errors"

// SerializationError signals that a value (typically a provider model's
// configuration, for workflow step boundaries) could not be serialized.
//
// Mirrors the TypeScript SDK's AI_SerializationError.
type SerializationError struct {
	// Message describes the problem. When empty, Error() falls back to the
	// TypeScript default message.
	Message string

	// Cause is the underlying error, when available.
	Cause error
}

// NewSerializationError creates a SerializationError. An empty message falls
// back to the TypeScript default ("Failed to serialize value.").
func NewSerializationError(message string, cause error) *SerializationError {
	return &SerializationError{Message: message, Cause: cause}
}

func (e *SerializationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "Failed to serialize value."
}

func (e *SerializationError) Unwrap() error {
	return e.Cause
}

// IsSerializationError checks if an error is a SerializationError.
func IsSerializationError(err error) bool {
	var target *SerializationError
	return errors.As(err, &target)
}
