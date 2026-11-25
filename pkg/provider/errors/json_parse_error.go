package errors

import (
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

// JSONParseError signals that text which should have been JSON could not be
// parsed (for example a malformed streaming frame, or a payload TS
// secureJsonParse rejects). It has Text (the text that failed) and Cause
// (the parser error); Error renders the TypeScript message
// `JSON parsing failed: Text: <text>.` followed by `\nError message: <cause>`.
// Callers match it with errors.As.
//
// Mirrors the TypeScript SDK's AI_JSONParseError. It is defined next to the
// central JSON decoders so they can return it.
type JSONParseError = jsonexact.JSONParseError

// NewJSONParseError creates a JSONParseError for text and the parser error.
func NewJSONParseError(text string, cause error) *JSONParseError {
	return &JSONParseError{Text: text, Cause: cause}
}

// IsJSONParseError checks if an error is (or wraps) a JSONParseError.
func IsJSONParseError(err error) bool {
	var target *JSONParseError
	return errors.As(err, &target)
}
