package types

import "testing"

// TestR92integDecodeFileDataStringForgiving (cr-r92-provc-to-gen): TS
// convertBase64ToUint8Array maps base64url characters and decodes with atob
// (forgiving base64: ASCII whitespace ignored, padding optional).
func TestR92integDecodeFileDataStringForgiving(t *testing.T) {
	for in, want := range map[string]string{
		"aG\n k":        "hi",
		"aGk=":          "hi",
		"aGk":           "hi",
		" aGk= \t":      "hi",
		"_-8":           "\xff\xef",
		"":              "",
		"aGVsbG8=":      "hello",
		"aG\r\nVs\fbG8": "hello",
	} {
		got, err := DecodeFileDataString(in)
		if err != nil || string(got) != want {
			t.Errorf("DecodeFileDataString(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"a", "aG=k", "aGk==", "aGk= ", "aG*k"} {
		if got, err := DecodeFileDataString(in); err == nil {
			t.Errorf("DecodeFileDataString(%q) = %q, want an error", in, got)
		}
	}
}
