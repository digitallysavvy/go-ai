package types

import "testing"

// Round 91 gen tests (TS 2ebc78b654).

// core2-19: TS getMediaTypeFromUrl takes `pathname.split('.').pop()`, which
// is the whole pathname when it has no "." (convert-to-language-model-prompt.ts
// 884-901): new URL('x:png').pathname is "png".
func TestR91genMediaTypeFromURLWithoutDot(t *testing.T) {
	for in, want := range map[string]string{
		"x:png":                     "image/png",
		"https://e.com/a.PDF":       "application/pdf",
		"https://e.com/png":         "application/octet-stream",
		"https://e.com/a.":          "application/octet-stream",
		"https://e.com/":            "application/octet-stream",
		"not a url":                 "application/octet-stream",
		"https://e.com/x.png?q=a.b": "image/png",
	} {
		if got := inferToolResultMediaTypeFromURL(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
