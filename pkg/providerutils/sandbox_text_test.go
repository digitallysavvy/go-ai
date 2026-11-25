package providerutils

import "testing"

func TestExtractLines_TSParity(t *testing.T) {
	i := func(n int) *int { return &n }
	tests := []struct {
		name       string
		text       string
		start, end *int
		want       string
	}{
		{"unchanged", "a\nb\nc", nil, nil, "a\nb\nc"},
		{"lf range", "a\nb\nc\nd", i(2), i(3), "b\nc"},
		{"crlf", "a\r\nb\r\nc\r\nd", i(2), i(3), "b\r\nc"},
		{"cr", "a\rb\rc\rd", i(2), i(3), "b\rc"},
		{"end past eof", "a\nb\nc", i(2), i(99), "b\nc"},
		{"only end", "a\nb\nc", nil, i(2), "a\nb"},
		{"only start", "a\nb\nc", i(2), nil, "b\nc"},
		{"no breaks", "one-liner", i(1), i(1), "one-liner"},
		{"negative end (JS slice)", "a\nb\nc", nil, i(-1), "a\nb"},
		{"start past eof", "a\nb", i(5), nil, ""},
		{"zero end", "a\nb", nil, i(0), ""},
	}
	for _, tt := range tests {
		if got := ExtractLines(tt.text, tt.start, tt.end); got != tt.want {
			t.Errorf("%s: got %q want %q", tt.name, got, tt.want)
		}
	}
}
