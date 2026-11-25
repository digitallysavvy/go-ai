package deepgram

import (
	"net/http"
	"testing"
)

// TestR91provcHeaderNumberKeepsLargeValues (provc-03): a header count above
// 2^31-1 keeps its value on 32-bit builds (an int64 there).
func TestR91provcHeaderNumberKeepsLargeValues(t *testing.T) {
	h := http.Header{}
	h.Set("Dg-Char-Count", "3851223452")
	var got float64
	switch v := headerNumber(h, "Dg-Char-Count").(type) {
	case int:
		got = float64(v)
	case int64:
		got = float64(v)
	default:
		t.Fatalf("headerNumber = %T", v)
	}
	if got != 3851223452 {
		t.Errorf("headerNumber = %v", got)
	}
	h.Set("Dg-Char-Count", "12")
	if _, ok := headerNumber(h, "Dg-Char-Count").(int); !ok {
		t.Error("an in-range value must stay an int (v0.5.2 type)")
	}
}
