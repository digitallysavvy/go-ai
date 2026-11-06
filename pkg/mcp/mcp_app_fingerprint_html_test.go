package mcp

import "testing"

// The digest covers the resource html, which is full of <, > and &: TS
// JSON.stringify writes them as is (and U+2028 raw), so the digest must not
// be taken over Go's HTML-escaped JSON. The expected value was computed with
// the TS algorithm in Node.
func TestFingerprintMCPAppResourceMatchesTSForHTML(t *testing.T) {
	got := FingerprintMCPAppResource(MCPAppResource{
		HTML: "<a href=\"x\">&amp;</a>  é",
		Meta: map[string]interface{}{
			"csp":         map[string]interface{}{"connectDomains": []interface{}{"https://a.b"}},
			"permissions": map[string]interface{}{"camera": map[string]interface{}{}},
		},
	})
	if want := "F3DKQXTJ50v3FcAK8Y5LNYrJmvrC6aWJOZ2qEvTMKlk"; got != want {
		t.Fatalf("fingerprint = %s, want %s", got, want)
	}
}
