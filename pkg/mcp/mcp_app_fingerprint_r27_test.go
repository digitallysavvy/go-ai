package mcp

import "testing"

// Expected digest computed with the TS canonicalJSON (keys sorted by UTF-16
// code unit) under Node: an astral key sorts before U+E000.
func TestFingerprintMCPAppResourceSortsKeysByUTF16(t *testing.T) {
	got := FingerprintMCPAppResource(MCPAppResource{
		HTML: "h",
		Meta: map[string]interface{}{"csp": map[string]interface{}{"": 1, "\U00010000": 2}},
	})
	if want := "UkNWrRSAMrKoc7ajGVnpeEwXP_J4zV-pMSPRDnB7CkA"; got != want {
		t.Fatalf("fingerprint = %s, want %s", got, want)
	}
}
