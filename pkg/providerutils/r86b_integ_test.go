package providerutils

import (
	"net/http"
	"testing"
)

// cr-r86b-tools-to-integrator 4: TS response headers are
// Object.fromEntries(response.headers.entries()): repeated lines joined with
// ", " and each byte one character (node fetch reads `X-U: \xc3\xa9z` as
// "Ã©z"). FetchURL is TS fetch's new URL(input).href / TypeError text.
func TestR86bintegExtractHeadersAndFetchURL(t *testing.T) {
	h := ExtractHeaders(http.Header{"X-U": {"\xc3\xa9z"}, "X-A": {"a", "b"}, "X-P": {"plain"}})
	if h["X-U"] != "Ã©z" || h["X-A"] != "a, b" || h["X-P"] != "plain" {
		t.Errorf("ExtractHeaders = %q", h)
	}
	for _, tc := range []struct{ in, want, err string }{
		{"https://h.example/cafÃ©?q=é", "https://h.example/caf%C3%83%C2%A9?q=%C3%A9", ""},
		{"https://H.example:443/a/../b", "https://h.example/b", ""},
		{"http://[::1", "", "Failed to parse URL from http://[::1"},
	} {
		got, err := FetchURL(tc.in)
		if got != tc.want || (err == nil) != (tc.err == "") || (err != nil && err.Error() != tc.err) {
			t.Errorf("FetchURL(%q) = %q, %v; want %q, %q", tc.in, got, err, tc.want, tc.err)
		}
	}
}
