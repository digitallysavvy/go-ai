package fileutil

import "testing"

// TS #23653: a trustedOrigin on a .internal host stays allowed; any other
// .internal origin is still validated (and blocked).
func TestTrustedOriginInternalHostValidator(t *testing.T) {
	const trusted = "https://service.internal"
	opts := TrustedOriginDownloadOptions(trusted, nil)
	if err := opts.URLValidator(trusted + "/final"); err != nil {
		t.Fatalf("trusted origin should skip validation: %v", err)
	}
	if err := opts.URLValidator("https://other.internal/file"); err == nil {
		t.Fatal("other .internal origin must be blocked")
	}
}
