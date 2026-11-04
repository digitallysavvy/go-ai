package harnessutil

import (
	"strings"
	"testing"
)

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

// TS getInlineImage string branch (`new URL(data)`, then getDataUrlImage on
// `url.href.split(',', 2)`); expected values from Node 24.
func TestR87toolsInlineImageFromStringWHATWG(t *testing.T) {
	cases := []struct {
		in, data, mediaType, errPart string
	}{
		{" data:image/png;base64,AAAA", "AAAA", "image/png", ""},
		{"data:image/png;base64,AA\nAA", "AAAA", "image/png", ""},
		{"DATA:image/png;base64,AAAA,BBB", "AAAA", "image/png", ""},
		{"http://[bad", "http://[bad", "", ""},
		{"abc:def", "", "", "remote image URLs"},
		{"AAAA", "AAAA", "", ""},
		{"data:text/plain,hi", "", "", "data URLs that are not base64 encoded"},
	}
	for _, tc := range cases {
		img, err := inlineImageFromString(tc.in)
		if tc.errPart != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errPart) {
				t.Errorf("%q: err = %v, want %q", tc.in, err, tc.errPart)
			}
			continue
		}
		if err != nil || img.data != tc.data || img.mediaType != tc.mediaType {
			t.Errorf("%q: %+v, %v; want data %q media type %q", tc.in, img, err, tc.data, tc.mediaType)
		}
	}
}
