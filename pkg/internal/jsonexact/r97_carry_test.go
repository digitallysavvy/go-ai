package jsonexact

import (
	"errors"
	"testing"
)

// TS JSONParseError renders its cause with getErrorMessage: Error#toString
// for the secure-json-parse SyntaxError (node: safeParseJSON({text:
// '{"__proto__":{"a":1}}'}) -> `...Error message: SyntaxError: Object
// contains forbidden prototype property`) and `unknown error` for no cause.
func TestR97carryJSONParseErrorCauseText(t *testing.T) {
	body := `{"__proto__":{"a":1}}`
	err := Unmarshal([]byte(body), &map[string]any{})
	want := "JSON parsing failed: Text: " + body + ".\nError message: SyntaxError: Object contains forbidden prototype property"
	if err == nil || err.Error() != want {
		t.Fatalf("Unmarshal err = %v, want %q", err, want)
	}
	if !errors.Is(err, ErrForbiddenPrototype) {
		t.Fatal("errors.Is(err, ErrForbiddenPrototype) = false")
	}
	if got := (&JSONParseError{Text: "x"}).Error(); got != "JSON parsing failed: Text: x.\nError message: unknown error" {
		t.Fatalf("nil cause Error() = %q", got)
	}
	if got := (&JSONParseError{Text: "x", Cause: errors.New("boom")}).Error(); got != "JSON parsing failed: Text: x.\nError message: boom" {
		t.Fatalf("plain cause Error() = %q", got)
	}
}
