package errors

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// core2-01: TS type-validation-error.ts 109-115 compares with === and
// optional chaining; Go must not panic on uncomparable values.
func TestR91carryWrapValidationErrorUncomparableValues(t *testing.T) {
	v := map[string]interface{}{"a": 1}
	ctx := &ValidationContext{Field: "f"}
	inner := NewValidationErrorWithContext(v, "bad", nil, &ValidationContext{Field: "f"})
	got := WrapValidationError(v, inner, ctx)
	if got != inner {
		t.Fatalf("same map and context: want the cause back, got %#v", got)
	}
	other := map[string]interface{}{"a": 1}
	if got := WrapValidationError(other, inner, ctx); got == inner {
		t.Fatal("a different map (JS !==) must give a new error")
	}
	s := []interface{}{1, 2}
	innerS := NewValidationErrorWithContext(s, "bad", nil, nil)
	if got := WrapValidationError(s, innerS, nil); got != innerS {
		t.Fatalf("same slice, nil contexts: want the cause, got %#v", got)
	}
	type holder struct{ X interface{} }
	h := holder{X: map[string]int{}}
	innerH := NewValidationErrorWithContext(h, "bad", nil, nil)
	_ = WrapValidationError(h, innerH, nil) // must not panic
}

func TestR91carryWrapValidationErrorNilContexts(t *testing.T) {
	ve := NewValidationErrorWithContext("x", "bad", nil, nil)
	if got := WrapValidationError("x", ve, nil); got != ve {
		t.Fatalf("nil contexts on both sides: want the cause, got %#v", got)
	}
	if got := WrapValidationError("x", ve, &ValidationContext{}); got != ve {
		t.Fatalf("nil vs empty context (all undefined in TS): want the cause, got %#v", got)
	}
	if got := WrapValidationError("x", ve, &ValidationContext{Field: "f"}); got == ve {
		t.Fatal("different field must give a new error")
	}
}

// core2-20: TS download-error.ts 19-21, the cause wins over the status.
func TestR91carryDownloadErrorCauseWins(t *testing.T) {
	got := NewDownloadError("u", 200, "OK", "", errors.New("unexpected EOF")).Error()
	if got != "Failed to download u: unexpected EOF" {
		t.Fatalf("Error() = %q", got)
	}
	if got := NewDownloadError("u", 404, "Not Found", "", nil).Error(); got != "Failed to download u: 404 Not Found" {
		t.Fatalf("Error() = %q", got)
	}
}

type r91carryStatusErr struct{ h http.Header }

func (e *r91carryStatusErr) Error() string { return "HTTP 429" }
func (e *r91carryStatusErr) HTTPStatusDetails() (int, map[string]string, []byte) {
	m := map[string]string{}
	for k, v := range e.h {
		m[strings.ToLower(k)] = v[0]
	}
	return 429, m, []byte(`{"error":{"message":"slow"}}`)
}
func (e *r91carryStatusErr) HTTPResponseHeader() http.Header { return e.h }

// core2-04: NewProviderError and NewSchemaProviderError use the TS
// lower-case keys (extract-response-headers.ts 7); NewProviderErrorFromResponse
// keeps the v0.5.2 canonical keys (S6). ResponseHeader reads either.
func TestR91carryResponseHeaderCasing(t *testing.T) {
	cause := func() error {
		return fmt.Errorf("w: %w", &r91carryStatusErr{h: http.Header{"Retry-After": {"7"}, "X-Request-Id": {"r1"}}})
	}
	lowerWant := map[string]string{"retry-after": "7", "x-request-id": "r1"}
	a := NewProviderError("p", 0, "", "", cause())
	b := NewSchemaProviderError("p", cause(), nil)
	c := NewProviderErrorFromResponse("p", 0, "", "m", cause())
	if !reflect.DeepEqual(a.ResponseHeaders, lowerWant) {
		t.Fatalf("NewProviderError headers = %#v", a.ResponseHeaders)
	}
	if !reflect.DeepEqual(b.ResponseHeaders, lowerWant) {
		t.Fatalf("NewSchemaProviderError headers = %#v", b.ResponseHeaders)
	}
	if c.ResponseHeaders["Retry-After"] != "7" {
		t.Fatalf("NewProviderErrorFromResponse headers = %#v", c.ResponseHeaders)
	}
	for _, e := range []*ProviderError{a, b, c} {
		if e.ResponseHeader("retry-after") != "7" || e.ResponseHeader("X-REQUEST-ID") != "r1" || e.ResponseHeader("missing") != "" {
			t.Fatalf("ResponseHeader lookups failed on %#v", e.ResponseHeaders)
		}
	}
	var nilErr *ProviderError
	if nilErr.ResponseHeader("x") != "" {
		t.Fatal("nil receiver")
	}
}
