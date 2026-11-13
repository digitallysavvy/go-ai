package posixpath

import "testing"

func TestDirnameDoubleLeadingSlash(t *testing.T) {
	for in, want := range map[string]string{"//a": "//", "///a": "//", "/a": "/", "a//b": "a/"} {
		if got := Dirname(in); got != want {
			t.Errorf("Dirname(%q) = %q, want %q", in, got, want)
		}
	}
}
