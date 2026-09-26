package errors

import (
	"encoding/json"
	"errors"
	"fmt"
)

// InvalidResponseDataError signals that a model returned a response whose data
// content is invalid (for example an embedding call that returned no
// embeddings, or a rerank ranking with an out-of-range index).
//
// Mirrors the TypeScript SDK's AI_InvalidResponseDataError.
type InvalidResponseDataError struct {
	// Data is the invalid response data.
	Data interface{}

	// Message describes the problem. When empty, Error() renders
	// `Invalid response data: <json>.` like the TypeScript default.
	Message string
}

// NewInvalidResponseDataError creates an InvalidResponseDataError. An empty
// message falls back to the TypeScript default message.
func NewInvalidResponseDataError(data interface{}, message string) *InvalidResponseDataError {
	return &InvalidResponseDataError{Data: data, Message: message}
}

func (e *InvalidResponseDataError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	b, err := json.Marshal(e.Data)
	if err != nil {
		return fmt.Sprintf("Invalid response data: %v.", e.Data)
	}
	return fmt.Sprintf("Invalid response data: %s.", string(b))
}

// IsInvalidResponseDataError checks if an error is an InvalidResponseDataError.
func IsInvalidResponseDataError(err error) bool {
	var target *InvalidResponseDataError
	return errors.As(err, &target)
}
