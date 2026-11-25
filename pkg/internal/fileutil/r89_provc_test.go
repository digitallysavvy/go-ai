package fileutil

import (
	"testing"
	"time"
)

// NoTimeout disables the client timeout (TS download sets none); zero keeps
// the 60 s default.
func TestR89provcNoTimeout(t *testing.T) {
	if c := NewDownloadClient("https://example.com", DownloadOptions{Timeout: NoTimeout}); c.Timeout != 0 {
		t.Fatalf("NoTimeout client timeout = %v", c.Timeout)
	}
	if c := NewDownloadClient("https://example.com", DefaultDownloadOptions()); c.Timeout != 60*time.Second {
		t.Fatalf("default client timeout = %v", c.Timeout)
	}
}

// TrustedOriginDownloadOptions is the getFromApi twin: TS sets no timeout.
func TestR89provcTrustedOriginNoTimeout(t *testing.T) {
	if opts := TrustedOriginDownloadOptions("https://api.example.com", nil); opts.Timeout != NoTimeout {
		t.Fatalf("timeout = %v", opts.Timeout)
	}
}
