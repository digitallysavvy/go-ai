package mcp

import "testing"

// TestMCPProtocolVersion20251125Supported verifies that "2025-11-25" is in the
// supported versions list and remains the legacy `initialize` handshake's
// preferred version (hash e6a9927 added a newer "2026-07-28" modern-era
// version negotiated via `server/discover`, ahead of it in preference order).
func TestMCPProtocolVersion20251125Supported(t *testing.T) {
	const want = "2025-11-25"

	// ProtocolVersion is an alias of LatestLegacyProtocolVersion.
	if ProtocolVersion != want {
		t.Errorf("ProtocolVersion = %q, want %q", ProtocolVersion, want)
	}
	if LatestLegacyProtocolVersion != want {
		t.Errorf("LatestLegacyProtocolVersion = %q, want %q", LatestLegacyProtocolVersion, want)
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

	// Must be second (immediately after the modern-era LatestProtocolVersion).
	if len(SupportedProtocolVersions) > 1 && SupportedProtocolVersions[1] != want {
		t.Errorf("SupportedProtocolVersions[1] = %q, want %q (legacy handshake preference)", SupportedProtocolVersions[1], want)
	}
}

// TestMCPLatestProtocolVersionIsPreferred verifies the 2026-07-28 modern-era
// protocol version (hash e6a9927, negotiated via `server/discover`) is first
// in SupportedProtocolVersions.
func TestMCPLatestProtocolVersionIsPreferred(t *testing.T) {
	const want = "2026-07-28"
	if LatestProtocolVersion != want {
		t.Errorf("LatestProtocolVersion = %q, want %q", LatestProtocolVersion, want)
	}
	if len(SupportedProtocolVersions) == 0 || SupportedProtocolVersions[0] != want {
		t.Errorf("SupportedProtocolVersions[0] = %v, want %q (newest first)", SupportedProtocolVersions, want)
	}
}

// TestMCPSupportedProtocolVersionsComplete verifies the full expected set.
func TestMCPSupportedProtocolVersionsComplete(t *testing.T) {
	want := []string{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}
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
