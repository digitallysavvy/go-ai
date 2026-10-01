package errors

import (
	"errors"
	"testing"
)

func TestSerializationError_DefaultMessage(t *testing.T) {
	err := NewSerializationError("", nil)
	if got := err.Error(); got != "Failed to serialize value." {
		t.Fatalf("Error() = %q, want default TS message", got)
	}
}

func TestSerializationError_CustomMessage(t *testing.T) {
	err := NewSerializationError("could not serialize headers", nil)
	if got := err.Error(); got != "could not serialize headers" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestSerializationError_Unwrap(t *testing.T) {
	cause := errors.New("boom")
	err := NewSerializationError("failed", cause)
	if !errors.Is(err, cause) {
		t.Fatal("expected errors.Is to find the wrapped cause")
	}
}

func TestIsSerializationError(t *testing.T) {
	err := NewSerializationError("failed", nil)
	if !IsSerializationError(err) {
		t.Fatal("expected IsSerializationError(SerializationError) to be true")
	}
	if IsSerializationError(errors.New("other")) {
		t.Fatal("expected IsSerializationError(other) to be false")
	}
}
