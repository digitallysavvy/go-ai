package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestJSONParseErrorMessageAndMatching(t *testing.T) {
	cause := errors.New("bad token")
	err := fmt.Errorf("wrapped: %w", NewJSONParseError("{x", cause))
	if !IsJSONParseError(err) || !errors.Is(err, cause) {
		t.Fatal("errors.As / Unwrap must reach the JSONParseError and its cause")
	}
	if got, want := NewJSONParseError("{x", cause).Error(), "JSON parsing failed: Text: {x.\nError message: bad token"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
