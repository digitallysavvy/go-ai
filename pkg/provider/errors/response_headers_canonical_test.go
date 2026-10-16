package errors

import (
	"fmt"
	"net/http"
	"testing"
)

type rawHeaderHTTPError struct {
	status int
	header http.Header
	body   []byte
}

func (e *rawHeaderHTTPError) Error() string { return fmt.Sprintf("HTTP %d", e.status) }
func (e *rawHeaderHTTPError) HTTPStatusDetails() (int, map[string]string, []byte) {
	lower := map[string]string{}
	for k, v := range e.header {
		lower[k] = v[0]
	}
	return e.status, lower, e.body
}
func (e *rawHeaderHTTPError) HTTPResponseHeader() http.Header { return e.header }

// NewProviderErrorFromResponse keeps the v0.5.2 ResponseHeaders shape of the
// providers it replaced: Go canonical keys and repeated values joined by ", ".
func TestNewProviderErrorFromResponseKeepsCanonicalHeaders(t *testing.T) {
	cause := &rawHeaderHTTPError{status: 400, body: []byte(`{}`), header: http.Header{
		"X-Request-Id": {"r1"},
		"Set-Cookie":   {"a=1", "b=2"},
	}}
	e := NewProviderErrorFromResponse("p", 0, "", "m", fmt.Errorf("wrapped: %w", cause))
	if e.ResponseHeaders["X-Request-Id"] != "r1" || e.ResponseHeaders["Set-Cookie"] != "a=1, b=2" {
		t.Fatalf("ResponseHeaders = %#v", e.ResponseHeaders)
	}
	if _, lower := e.ResponseHeaders["x-request-id"]; lower {
		t.Fatalf("lower-case key present: %#v", e.ResponseHeaders)
	}
}
