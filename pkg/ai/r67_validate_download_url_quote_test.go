package ai

import "testing"

// TS validateDownloadUrl: `URL with hostname ${hostname} is not allowed`,
// the hostname unquoted.
func TestValidateDownloadURL_R67_HostnameMessageUnquoted(t *testing.T) {
	err := validateDownloadURL("http://printer.local/x.png")
	if err == nil {
		t.Fatal("expected .local hostname to be blocked")
	}
	if got, want := err.Error(), "URL with hostname printer.local is not allowed"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
