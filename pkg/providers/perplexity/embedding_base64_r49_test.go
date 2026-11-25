package perplexity

import (
	"reflect"
	"testing"
)

// TS decodeEmbedding uses convertBase64ToUint8Array: base64url characters are
// mapped to the standard alphabet and atob's forgiving decode accepts missing
// padding and ASCII whitespace.
func TestDecodePerplexityEmbeddingForgivingBase64(t *testing.T) {
	// bytes 0xfb 0xff 0xfe -> int8 -5, -1, -2; standard "+//+", url "-__-".
	cases := map[string][]float64{
		"+//+":   {-5, -1, -2},
		"-__-":   {-5, -1, -2},
		"AQI":    {1, 2},    // unpadded
		"AQI=":   {1, 2},    // padded
		"A Q\nI": {1, 2},    // whitespace ignored
		"":       {},        // empty
		"AQID":   {1, 2, 3}, // no padding needed
		"gH8=":   {-128, 127},
	}
	for in, want := range cases {
		got, err := decodePerplexityEmbedding(in, "base64_int8")
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
	if _, err := decodePerplexityEmbedding("A", "base64_int8"); err == nil {
		t.Error("length 1 mod 4 must fail like atob")
	}
}
