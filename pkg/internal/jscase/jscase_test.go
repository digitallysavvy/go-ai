package jscase

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// The tables and the Final_Sigma rule against Node 24 (Unicode 17):
// testdata/node_case.json.gz holds String.fromCodePoint(c).toLowerCase()
// and toUpperCase() for every code point whose case changes, plus context
// strings, printed by a script over 0..0x10FFFF.
func TestMatchesNodeOverEveryCodePoint(t *testing.T) {
	f, err := os.Open("testdata/node_case.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Lower, Upper map[string][]rune
		Ctx          [][3]string
	}
	if err := json.NewDecoder(zr).Decode(&d); err != nil {
		t.Fatal(err)
	}
	bad := 0
	for c := rune(0); c <= 0x10ffff; c++ {
		if c >= 0xd800 && c <= 0xdfff {
			continue
		}
		key := strconv.Itoa(int(c))
		wantLower, wantUpper := string(c), string(c)
		if v, ok := d.Lower[key]; ok {
			wantLower = string(v)
		}
		if v, ok := d.Upper[key]; ok {
			wantUpper = string(v)
		}
		if got := ToLower(string(c)); got != wantLower && bad < 20 {
			bad++
			t.Errorf("ToLower(U+%04X) = %+q, want %+q", c, got, wantLower)
		}
		if got := ToUpper(string(c)); got != wantUpper && bad < 20 {
			bad++
			t.Errorf("ToUpper(U+%04X) = %+q, want %+q", c, got, wantUpper)
		}
	}
	for _, c := range d.Ctx {
		if got := ToLower(c[0]); got != c[1] {
			t.Errorf("ToLower(%+q) = %+q, want %+q", c[0], got, c[1])
		}
		if got := ToUpper(c[0]); got != c[2] {
			t.Errorf("ToUpper(%+q) = %+q, want %+q", c[0], got, c[2])
		}
	}
}
