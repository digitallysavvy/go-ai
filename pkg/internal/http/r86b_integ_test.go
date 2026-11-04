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
	if h["retry-after"] != "1, 2" || h["x-u"] != "\u00c3\u00a9z" {
		t.Errorf("headers = %q, want retry-after \"1, 2\" and x-u \"\u00c3\u00a9z\"", h)
	}
}

// cr-r86b-prova-to-carry: one URLSearchParams implementation. Wants printed by
// Node 24 (`u = new URL('https://h/p?' + q); u.searchParams.set(k, v); u.search`).
func TestR86bintegSetSearchParamMatchesURLSearchParams(t *testing.T) {
	for _, tc := range []struct{ q, k, v, want string }{
		{"b=1%202&a=~", "key", "a~b*c d", "b=1+2&a=%7E&key=a%7Eb*c+d"},
		{"a=1&key=x&key=y&b=%zz", "key", "v", "a=1&key=v&b=%25zz"},
		{"x=%E9&y=+&z=%C3%A9", "q", "\u00e9!'()", "x=%EF%BF%BD&y=+&z=%C3%A9&q=%C3%A9%21%27%28%29"},
		{"", "k", "1", "k=1"},
	} {
		if got := SetSearchParam(tc.q, tc.k, tc.v); got != tc.want {
			t.Errorf("SetSearchParam(%q, %q, %q) = %q, want %q", tc.q, tc.k, tc.v, got, tc.want)
		}
	}
}
