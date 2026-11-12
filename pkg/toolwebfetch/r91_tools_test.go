package toolwebfetch

import (
	"strings"
	"testing"
)

// tools-03 (TS web-fetch-tool.ts:127 sliceDocument, :187 url.length): JS
// strings count UTF-16 code units.
func TestR91toolsWebFetchUTF16Lengths(t *testing.T) {
	ft := fixed(200, "a😀b", "Content-Type", "text/plain")
	if d := doc(t, run(t, newTool(t, Options{Transport: ft}), "https://example.com/", 0)); d.TotalLength != 4 || d.Content != "a😀b" {
		t.Fatalf("document = %+v", d)
	}
	d := doc(t, run(t, newTool(t, Options{Transport: fixed(200, "a😀b", "Content-Type", "text/plain"), MaxLength: intp(2)}), "https://example.com/", 0))
	if d.Content != "a" || d.NextOffset == nil || *d.NextOffset != 1 {
		t.Fatalf("maxLength 2 = %+v", d)
	}
	// The 20-unit prefix plus 1015 emoji is 1035 code points but 2050 units
	// (over the 2048 limit); 1014 emoji is exactly 2048 units.
	long := "https://example.com/" + strings.Repeat("😀", 1015)
	if _, invalid := validateURL(long); invalid == nil {
		t.Fatalf("a %d-unit URL was accepted", utf16Length(long))
	}
	if _, invalid := validateURL("https://example.com/" + strings.Repeat("😀", 1014)); invalid != nil {
		t.Fatalf("a 2048-unit URL was rejected: %+v", invalid)
	}
}
