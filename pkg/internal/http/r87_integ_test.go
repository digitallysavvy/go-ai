package http

import (
	"net/http"
	"testing"
)

// TS APICallError.responseHeaders (extractResponseHeaders): the last
// Set-Cookie line wins, other repeated headers are joined.
func TestR87integHTTPStatusDetailsSetCookieLastLine(t *testing.T) {
	_, h, _ := (&HTTPStatusError{StatusCode: 400, Headers: http.Header{"Set-Cookie": {"a=1", "b=2"}, "X-A": {"1", "2"}}}).HTTPStatusDetails()
	if h["set-cookie"] != "b=2" || h["x-a"] != "1, 2" {
		t.Errorf("headers = %#v", h)
	}
}
