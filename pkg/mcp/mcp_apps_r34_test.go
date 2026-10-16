package mcp

import "testing"

// atob (WHATWG forgiving-base64) drops trailing "=" only when the length is a
// multiple of four, and never accepts "=" elsewhere.
func TestDecodeBase64LenientMatchesAtobPadding(t *testing.T) {
	ok := map[string]string{"YQ==": "a", "YQ": "a", "YWI=": "ab", "YWI": "ab", "YWJj": "abc", "YQ ==": "a", "Y Q==": "a", "": ""}
	for in, want := range ok {
		got, err := decodeBase64Lenient(in)
		if err != nil || string(got) != want {
			t.Errorf("decodeBase64Lenient(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"YQ=", "YQ===", "YWJj====", "YWJj=", "Y", "=", "===="} {
		if _, err := decodeBase64Lenient(in); err == nil {
			t.Errorf("decodeBase64Lenient(%q) accepted, atob throws", in)
		}
	}
}
