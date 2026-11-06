package errors

import (
	"errors"
	"testing"
)

// TS has two AI_InvalidArgumentError classes: `ai`'s prefixes the message
// with `Invalid argument for parameter <x>: `; `@ai-sdk/provider`'s
// (NewArgumentError) is the bare message (cr-r87e-gen-to-carry CR 4).
func TestR87eIntegNewArgumentErrorIsBare(t *testing.T) {
	cause := errors.New("why")
	err := NewArgumentError("questions", "Decision requires at least one question.", cause)
	if got := err.Error(); got != "Decision requires at least one question." {
		t.Fatalf("Error() = %q", got)
	}
	var ia *InvalidArgumentError
	if !errors.As(error(err), &ia) || ia.Field != "questions" || ia.Message != "Decision requires at least one question." {
		t.Fatalf("errors.As = %#v", ia)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause not reachable")
	}
	lit := &InvalidArgumentError{Field: "questions", Message: "Decision requires at least one question."}
	if got := lit.Error(); got != "Invalid argument for parameter questions: Decision requires at least one question." {
		t.Fatalf("struct literal Error() = %q", got)
	}
}
