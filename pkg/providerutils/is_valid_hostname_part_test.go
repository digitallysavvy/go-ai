package providerutils

import (
	"strings"
	"testing"
)

// TestIsValidHostnamePart ports TS provider-utils
// is-valid-hostname-part.test.ts: it must accept exactly a single ASCII DNS
// label and reject anything that could redirect a generated request host
// (e.g. "user@internal:8080/#").
func TestIsValidHostnamePart(t *testing.T) {
	accept := []string{"a", "0", "us-east-1", "MY-resource", "global", strings.Repeat("a", 63)}
	for _, v := range accept {
		if !IsValidHostnamePart(v) {
			t.Errorf("IsValidHostnamePart(%q) = false, want true", v)
		}
	}

	reject := []string{
		"",
		strings.Repeat("a", 64),
		"-a",
		"a-",
		"a.b",
		"a_b",
		"é",
		"user@internal:8080/#",
		"evil.example.com/#",
		"169.254.169.254:80/x#",
		"us-east-1/../..",
		"us east 1",
		"a\n",
		"a\r",
		"a\t",
		"a\x00",
		"%61",
	}
	for _, v := range reject {
		if IsValidHostnamePart(v) {
			t.Errorf("IsValidHostnamePart(%q) = true, want false", v)
		}
	}
}
