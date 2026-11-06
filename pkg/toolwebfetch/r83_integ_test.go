package toolwebfetch

import "testing"

// TestR83integDecodeTextMatchesTextDecoder: TS content-type.ts decodes with
// new TextDecoder(charset).decode(body): a truncated sequence is one U+FFFD
// (Node: 41 F0 9F 98 -> "A\uFFFD"), and only the first UTF-8 BOM is dropped.
func TestR83integDecodeTextMatchesTextDecoder(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		ct   string
		want string
	}{
		{"truncated", []byte{0x41, 0xF0, 0x9F, 0x98}, "text/plain", "A\uFFFD"},
		{"invalid run", []byte{0x41, 0xE0, 0x80, 0x42}, "text/plain; charset=utf-8", "A\uFFFD\uFFFDB"},
		{"double bom", []byte{0xEF, 0xBB, 0xBF, 0xEF, 0xBB, 0xBF, 0x41}, "text/plain", "\uFEFFA"},
		{"unknown charset", []byte{0x41, 0xF0, 0x9F, 0x98}, "text/plain; charset=x-nope", "A\uFFFD"},
	}
	for _, c := range cases {
		if got := decodeText(c.body, c.ct, false); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if !looksLikeHTML([]byte{0xEF, 0xBB, 0xBF, ' ', '<', 'h', 't', 'm', 'l', '>'}) {
		t.Error("BOM + whitespace + <html> not sniffed as HTML")
	}
}
