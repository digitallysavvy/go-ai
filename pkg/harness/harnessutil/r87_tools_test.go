package harnessutil

import "testing"

// Expected values are Node 24 `new URL(s).hostname` and
// the pathname without trailing slashes (TS createCredentialRequestTransformation).
func TestR87toolsCredentialTransformationWHATWGURL(t *testing.T) {
	cases := []struct{ in, host, path string }{
		{"https://API.Example.com/v1/", "api.example.com", "/v1"},
		{"https://x.com:443/a/../b//", "x.com", "/b"},
		{"https://[::1]:8080/v1", "[::1]", "/v1"},
		{"https://bücher.de/x y", "xn--bcher-kva.de", "/x%20y"},
		{"foo:bar", "", "bar"},
		{"https://user:pw@h.com/p%2fq/", "h.com", "/p%2fq"},
		{"http://x.com/%7e/", "x.com", "/%7e"},
		{"https://api.anthropic.com", "api.anthropic.com", ""},
	}
	for _, tc := range cases {
		got, err := CreateCredentialRequestTransformation(CreateCredentialRequestTransformationOptions{
			MatchURL: tc.in, MatchHeaders: map[string]string{"x-api-key": "p"}, TransformHeaders: map[string]string{"x-api-key": "r"},
		})
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		gotPath := ""
		if got.Match.Path != nil {
			gotPath = got.Match.Path.StartsWith
		}
		if got.Match.Host != tc.host || gotPath != tc.path {
			t.Errorf("%q: host %q path %q, want %q %q", tc.in, got.Match.Host, gotPath, tc.host, tc.path)
		}
	}
}
