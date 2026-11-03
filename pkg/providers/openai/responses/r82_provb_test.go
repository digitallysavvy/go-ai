package responses

import "testing"

// TS detectMediaType decodes only the sniffed prefix of a base64 string
// (decodePrefix): a bad tail does not stop the detection.
func TestR82provbInlineBase64SniffDecodesOnlyPrefix(t *testing.T) {
	got, err := resolveInlineBase64MediaType("image", "iVBORw0KGgoAAAANSUhEUgAA%%not base64%%")
	if err != nil || got != "image/png" {
		t.Fatalf("got %q, %v; want image/png", got, err)
	}
}
