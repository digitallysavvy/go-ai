package mcp

import "testing"

// TestMCPProtocolVersion20251125Supported verifies that "2025-11-25" is in the
// supported versions list and that it is the preferred (newest) version.
func TestMCPProtocolVersion20251125Supported(t *testing.T) {
	const want = "2025-11-25"

	// Must be the advertised preferred version.
	if ProtocolVersion != want {
		t.Errorf("ProtocolVersion = %q, want %q", ProtocolVersion, want)
	}

	// Must appear in SupportedProtocolVersions.
	found := false
	for _, v := range SupportedProtocolVersions {
		if v == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%q not found in SupportedProtocolVersions %v", want, SupportedProtocolVersions)
	}

	// Must be first (highest preference).
	if len(SupportedProtocolVersions) > 0 && SupportedProtocolVersions[0] != want {
		t.Errorf("SupportedProtocolVersions[0] = %q, want %q (newest first)", SupportedProtocolVersions[0], want)
	}
}

// TestMCPSupportedProtocolVersionsComplete verifies the full expected set.
func TestMCPSupportedProtocolVersionsComplete(t *testing.T) {
	want := []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}
	if len(SupportedProtocolVersions) != len(want) {
		t.Errorf("len(SupportedProtocolVersions) = %d, want %d: %v", len(SupportedProtocolVersions), len(want), SupportedProtocolVersions)
		return
	}
	for i, v := range want {
		if SupportedProtocolVersions[i] != v {
			t.Errorf("SupportedProtocolVersions[%d] = %q, want %q", i, SupportedProtocolVersions[i], v)
		}
	}
}
