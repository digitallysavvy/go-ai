package http

import (
	"net/http"
	"testing"
)

// cr-r86b-tools-to-carry 1: TS APICallError.responseHeaders is
// extractResponseHeaders(response): every line of a header joined with ", "
// and read as Latin-1 (node fetch: `X-U: \xc3\xa9z` reads "Ã©z"); Go kept the
// first raw value.
func TestR86bintegHTTPStatusDetailsHeadersAsFetch(t *testing.T) {
	e := &HTTPStatusError{StatusCode: 429, Headers: http.Header{
		"Retry-After": {"1", "2"},
		"X-U":         {"\xc3\xa9z"},
	}}
	_, h, _ := e.HTTPStatusDetails()
	if h["retry-after"] != "1, 2" || h["x-u"] != "Ã©z" {
		t.Errorf("headers = %q, want retry-after \"1, 2\" and x-u \"Ã©z\"", h)
	}
}
