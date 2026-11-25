package mcp

import (
	"encoding/base64"
	"testing"
)

// TS decodes a blob with `new TextDecoder().decode(bytes)`: the BOM is
// dropped and each maximal invalid subpart becomes one U+FFFD.
func TestGetMCPAppResourceFromReadResultBlobDecodesLikeTextDecoder(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"bom", []byte("\xEF\xBB\xBF<p>"), "<p>"},
		{"truncated 3-byte", []byte("\xE2\x82A"), "�A"},
		{"truncated 4-byte", []byte("\xF0\x9F\x98x"), "�x"},
		{"lone continuation", []byte("a\x80b"), "a�b"},
		{"overlong", []byte("\xC0\x80"), "��"},
		{"surrogate", []byte("\xED\xA0\x80"), "���"},
		{"valid", []byte("é😀"), "é😀"},
	}
	for _, tc := range cases {
		res, err := GetMCPAppResourceFromReadResult("ui://x", ReadResourceResult{Contents: []ResourceContent{{
			URI: "ui://x", MimeType: MCPAppMimeType, Blob: base64.StdEncoding.EncodeToString(tc.in),
		}}})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if res.HTML != tc.want {
			t.Errorf("%s: html = %q, want %q", tc.name, res.HTML, tc.want)
		}
	}
}
