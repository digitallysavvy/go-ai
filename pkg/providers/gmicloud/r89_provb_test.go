package gmicloud

import "testing"

// TS gmicloud-error.ts unwrapDetailsMessage parses details with
// secureJsonParse, so a __proto__ key keeps the outer message.
func TestR89provbDetailsSecureParse(t *testing.T) {
	if got := unwrapGmicloudDetailsMessage(`{"error":{"message":"inner"},"__proto__":{"x":1}}`); got != "" {
		t.Fatalf("inner = %q, want none", got)
	}
	if got := unwrapGmicloudDetailsMessage(`{"error":{"message":"inner"}}`); got != "inner" {
		t.Fatalf("inner = %q", got)
	}
}
