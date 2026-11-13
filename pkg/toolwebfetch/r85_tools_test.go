package toolwebfetch

import "testing"

// JS trim semantics (class sweep r85): node gives
// "\u0085text/html ".trim() === "\u0085text/html",
// "\ufefftext/html".trim() === "text/html", and trimStart keeps a leading
// U+0085 but drops U+FEFF before "<html".
func TestR85toolsJSTrimInContentType(t *testing.T) {
	if got := getMediaType("\u0085text/html ; charset=utf-8"); got != "\u0085text/html" {
		t.Fatalf("getMediaType NEL = %q", got)
	}
	if got := getMediaType("\ufeffText/HTML"); got != "text/html" {
		t.Fatalf("getMediaType BOM = %q", got)
	}
	if looksLikeHTML([]byte("\u0085<html>")) {
		t.Fatal("a leading U+0085 is not JS whitespace")
	}
	if !looksLikeHTML([]byte("  <!DOCTYPE html>")) {
		t.Fatal("NBSP is JS whitespace")
	}
	if _, err := normalizeDomains("allowedDomains", []string{"\ufeffexample.com"}); err != nil {
		t.Fatalf("a BOM-padded domain is trimmed like JS: %v", err)
	}
}
