package cursor

import "testing"

// TS VERSION of @ai-sdk/harness-cursor at 30548ee7f9 (package.json 1.0.88);
// Cursor has no embedded bridge, so the manifest cannot pin it.
func TestR42ClientAppVersionMatchesTSPackage(t *testing.T) {
	if ClientAppVersion != "1.0.91" {
		t.Fatalf("ClientAppVersion = %q, want the TS package version 1.0.88", ClientAppVersion)
	}
}
