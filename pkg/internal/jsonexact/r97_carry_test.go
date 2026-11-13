package jsonexact

import (
	"errors"
	"math"
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

// UnmarshalPlain is TS `await response.json()`: JSON.parse keeps __proto__
// and constructor.prototype as own properties (node: JSON.parse(
// '{"__proto__":{"a":1},"n":1e400}') has own key __proto__ and n Infinity),
// keys match exactly, and 1e400 reads as Infinity. Unmarshal stays strict.
func TestR97carryUnmarshalPlainKeepsPrototypeKeys(t *testing.T) {
	body := []byte(`{"__proto__":{"a":1},"constructor":{"prototype":2},"N":3,"n":1e400}`)
	var m map[string]any
	if err := UnmarshalPlain(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["__proto__"]; !ok || m["constructor"] == nil {
		t.Fatalf("UnmarshalPlain = %v, want the prototype keys kept", m)
	}
	if n, _ := m["n"].(float64); !math.IsInf(n, 1) {
		t.Fatalf("n = %v, want +Inf", m["n"])
	}
	var s struct {
		N any `json:"n"`
	}
	if err := UnmarshalPlain(body, &s); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.N.(float64); !math.IsInf(n, 1) {
		t.Fatalf("struct n = %v, want +Inf (not the case-folded N)", s.N)
	}
	if err := Unmarshal(body, &m); !errors.Is(err, ErrForbiddenPrototype) {
		t.Fatalf("Unmarshal err = %v, want the prototype rejection", err)
	}
}
