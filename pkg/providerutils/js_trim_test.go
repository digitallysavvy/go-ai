package providerutils

import "testing"

func TestJSTrim(t *testing.T) {
	cases := map[string]string{
		"\xef\xbb\xbf hi   ": "hi",
		"\u0085hi\u0085":         "\u0085hi\u0085", // NEL is not JS whitespace
		"᠎hi":               "᠎hi",       // Mongolian vowel separator is not whitespace since ES2016
		"  \t\n":                 "",
	}
	for in, want := range cases {
		if got := JSTrim(in); got != want {
			t.Errorf("JSTrim(%q) = %q, want %q", in, got, want)
		}
	}
}
