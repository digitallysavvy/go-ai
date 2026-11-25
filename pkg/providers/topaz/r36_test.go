package topaz

import (
	"bytes"
	"mime/multipart"
	"strings"
	"testing"
)

// TS appends numbers with String(value): 0.00001, not Go's 1e-05.
func TestWriteFloatFieldUsesJSNumberString(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	v := 0.00001
	if err := writeFloatField(w, "grainDensity", &v); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if !strings.Contains(buf.String(), "\r\n\r\n0.00001\r\n") {
		t.Fatalf("body = %q, want grainDensity 0.00001", buf.String())
	}
}
