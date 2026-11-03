package providerutils

import (
	"strings"
	"testing"
)

// stream CR: TS parseJsonLines decodes the stream with TextDecoder
// (response-handler.ts 279): the leading BOM is dropped, each maximal
// invalid subpart is one U+FFFD, CRLF rows lose the CR, and only the first
// row's BOM goes.
func TestR84integJSONLinesDecodedLikeTextDecoder(t *testing.T) {
	s := NewJSONLinesScanner(strings.NewReader("\xEF\xBB\xBF{\"a\":1}\r\n{\"v\":\"x\xF0\x9F\x98y\"}\n\xEF\xBB\xBF{}"), 1024)
	var rows []string
	for s.Scan() {
		rows = append(rows, s.Text())
	}
	if s.Err() != nil {
		t.Fatal(s.Err())
	}
	want := []string{`{"a":1}`, "{\"v\":\"x\uFFFDy\"}", "\uFEFF{}"}
	if strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
	// The limit still applies to the raw row.
	s = NewJSONLinesScanner(strings.NewReader("12345\n"), 4)
	for s.Scan() {
	}
	if s.Err() == nil {
		t.Fatal("oversized row accepted")
	}
}
