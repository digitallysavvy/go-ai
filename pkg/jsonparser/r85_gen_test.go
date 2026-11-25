package jsonparser

import "testing"

// Port of fix-json.test.ts 'should handle incomplete negative number as first
// element' (TS 8ba3a5c254).
func TestR85genFixJSONArrayStartMinus(t *testing.T) {
	for in, want := range map[string]string{
		"[-":       "[]",
		"[ -":      "[]",
		"[[1], [-": "[[1], []]",
		`{"a": [-`: `{"a": []}`,
		"[-1":      "[-1]",
	} {
		if got := FixJSON(in); got != want {
			t.Errorf("FixJSON(%q) = %q, want %q", in, got, want)
		}
	}
}
