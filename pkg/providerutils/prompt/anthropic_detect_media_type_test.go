package prompt

import "testing"

// TS resolveFullMediaType uses detectMediaType, which has no jpeg default and
// knows bmp/tiff/avif/heic; undetectable bytes must stay undetected.
func TestDetectAnthropicMediaTypeMatchesTS(t *testing.T) {
	bmp := []byte{0x42, 0x4d, 1, 2, 3, 4, 0, 0, 0, 0, 0, 0}
	for _, tc := range []struct {
		name string
		data []byte
		top  string
		want string
	}{
		{"bmp is not jpeg", bmp, "image", "image/bmp"},
		{"jpeg needs only FF D8", []byte{0xff, 0xd8, 0x00}, "image", "image/jpeg"},
		{"unknown image bytes undetected", []byte("hello world!"), "image", ""},
		{"pdf", []byte("%PDF-1.7"), "application", "application/pdf"},
		{"empty top-level type scans every table", []byte("%PDF-1.7"), "", "application/pdf"},
	} {
		if got := detectAnthropicMediaType(tc.data, tc.top); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
