package providerutils

import (
	"strings"
	"testing"
)

// Ports provider-utils/src/is-valid-hostname-part.test.ts.
func TestIsValidHostnamePart(t *testing.T) {
	t.Parallel()

	accepted := []string{"a", "0", "us-east-1", "MY-resource", "global", strings.Repeat("a", 63)}
	for _, value := range accepted {
		if !IsValidHostnamePart(value) {
			t.Errorf("IsValidHostnamePart(%q) = false, want true", value)
		}
	}

	rejected := []string{
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
	for _, value := range rejected {
		if IsValidHostnamePart(value) {
			t.Errorf("IsValidHostnamePart(%q) = true, want false", value)
		}
	}
}
