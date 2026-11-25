package providerutils

import "testing"

func TestJSTrim(t *testing.T) {
	cases := map[string]string{
		"\xef\xbb\xbf hi \u00a0\u2003": "hi",
		"\u0085hi\u0085":               "\u0085hi\u0085", // NEL is not JS whitespace
		"\u180ehi":                     "\u180ehi",       // Mongolian vowel separator is not whitespace since ES2016
		"  \t\n":                       "",
	}
	for in, want := range cases {
		if got := JSTrim(in); got != want {
			t.Errorf("JSTrim(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJSTrimStartAndEnd(t *testing.T) {
	in := "\ufeff\u00a0 x \u0085\u3000"
	if got := JSTrimStart(in); got != "x \u0085\u3000" {
		t.Fatalf("JSTrimStart = %q", got)
	}
	if got := JSTrimEnd(in); got != "\ufeff\u00a0 x \u0085" {
		t.Fatalf("JSTrimEnd = %q", got)
	}
}
