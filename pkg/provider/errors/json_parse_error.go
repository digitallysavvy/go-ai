package errors

import (
	"errors"
	"fmt"
)

// JSONParseError signals that text which should have been JSON could not be
// parsed (for example a malformed streaming frame). Callers match it with
// errors.As.
//
// Mirrors the TypeScript SDK's AI_JSONParseError.
type JSONParseError struct {
	// Text is the text that failed to parse.
	Text string

	// Cause is the underlying parser error.
	Cause error
}

// NewJSONParseError creates a JSONParseError for text and the parser error.
func NewJSONParseError(text string, cause error) *JSONParseError {
	return &JSONParseError{Text: text, Cause: cause}
}

// Error renders the TypeScript message: `JSON parsing failed: Text: <text>.`
// followed by `\nError message: <cause>`.
func (e *JSONParseError) Error() string {
	msg := fmt.Sprintf("JSON parsing failed: Text: %s.", e.Text)
	if e.Cause != nil {
		msg += "\nError message: " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the parser error.
func (e *JSONParseError) Unwrap() error { return e.Cause }

// IsJSONParseError checks if an error is (or wraps) a JSONParseError.
func IsJSONParseError(err error) bool {
	var target *JSONParseError
	return errors.As(err, &target)
}
