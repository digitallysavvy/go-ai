package cursor

import "testing"

// TS VERSION of @ai-sdk/harness-cursor at e64ed82f27 (package.json 1.0.78);
// Cursor has no embedded bridge, so the manifest cannot pin it.
func TestR42ClientAppVersionMatchesTSPackage(t *testing.T) {
	if ClientAppVersion != "1.0.78" {
		t.Fatalf("ClientAppVersion = %q, want the TS package version 1.0.78", ClientAppVersion)
	}
}
