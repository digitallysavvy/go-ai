package moonshot

import "testing"

// TestR86bprovbTokenCountHeaderJSTrim pins TS parseTokenCountHeader
// (/^\d+$/.test(value.trim())): JS trim removes U+FEFF and U+3000 but not
// U+0085. Each want was printed by Node 24.
func TestR86bprovbTokenCountHeaderJSTrim(t *testing.T) {
	for _, tc := range []struct {
		value string
		n     int64
		ok    bool
	}{
		{"12", 12, true},
		{" 12 ", 12, true},
		{"\ufeff12", 12, true},
		{"12\u0085", 0, false},
		{"\u00a012\u3000", 12, true},
		{"1 2", 0, false},
		{"", 0, false},
	} {
		n, ok := parseTokenCountHeader(tc.value)
		if n != tc.n || ok != tc.ok {
			t.Errorf("parseTokenCountHeader(%q) = %d, %v; want %d, %v", tc.value, n, ok, tc.n, tc.ok)
		}
	}
}
