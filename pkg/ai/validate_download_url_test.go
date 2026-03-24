package ai

import (
	"testing"
)

// ── Allowed URLs ──────────────────────────────────────────────────────────────

func TestValidateDownloadURL_AllowsHTTPS(t *testing.T) {
	if err := validateDownloadURL("https://example.com/image.png"); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateDownloadURL_AllowsHTTP(t *testing.T) {
	if err := validateDownloadURL("http://example.com/image.png"); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateDownloadURL_AllowsPublicIPAddress(t *testing.T) {
	// 203.0.113.1 is TEST-NET-3 (documentation range) — publicly routable.
	if err := validateDownloadURL("https://203.0.113.1/file"); err != nil {
		t.Errorf("expected public IP to be allowed, got: %v", err)
	}
}

func TestValidateDownloadURL_AllowsURLWithPort(t *testing.T) {
	if err := validateDownloadURL("https://example.com:8080/file"); err != nil {
		t.Errorf("expected URL with port to be allowed, got: %v", err)
	}
}

func TestValidateDownloadURL_AllowsDataURL(t *testing.T) {
	if err := validateDownloadURL("data:text/plain;base64,aGVsbG8="); err != nil {
		t.Errorf("expected data: URL to be allowed, got: %v", err)
	}
}

// ── Blocked protocols ─────────────────────────────────────────────────────────

func TestValidateDownloadURL_BlocksFileScheme(t *testing.T) {
	if err := validateDownloadURL("file:///etc/passwd"); err == nil {
		t.Error("expected error for file:// URL, got nil")
	}
}

func TestValidateDownloadURL_BlocksFTPScheme(t *testing.T) {
	if err := validateDownloadURL("ftp://example.com/file"); err == nil {
		t.Error("expected error for ftp:// URL, got nil")
	}
}

func TestValidateDownloadURL_BlocksJavascriptScheme(t *testing.T) {
	if err := validateDownloadURL("javascript:alert(1)"); err == nil {
		t.Error("expected error for javascript: URL, got nil")
	}
}

// ── Malformed URLs ────────────────────────────────────────────────────────────

func TestValidateDownloadURL_BlocksInvalidURL(t *testing.T) {
	// "not-a-url" has no scheme — should be blocked.
	if err := validateDownloadURL("not-a-url"); err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

// ── Blocked hostnames ─────────────────────────────────────────────────────────

func TestValidateDownloadURL_BlocksLocalhostByName(t *testing.T) {
	if err := validateDownloadURL("http://localhost/file"); err == nil {
		t.Error("expected error for localhost, got nil")
	}
}

func TestValidateDownloadURL_BlocksLocalhostWithPort(t *testing.T) {
	if err := validateDownloadURL("http://localhost:3000/file"); err == nil {
		t.Error("expected error for localhost:3000, got nil")
	}
}

func TestValidateDownloadURL_BlocksDotLocal(t *testing.T) {
	if err := validateDownloadURL("http://myhost.local/file"); err == nil {
		t.Error("expected error for .local domain, got nil")
	}
}

func TestValidateDownloadURL_BlocksDotLocalhost(t *testing.T) {
	if err := validateDownloadURL("http://app.localhost/file"); err == nil {
		t.Error("expected error for .localhost domain, got nil")
	}
}

// ── Blocked IPv4 addresses ────────────────────────────────────────────────────

func TestValidateDownloadURL_BlocksLoopback127_0_0_1(t *testing.T) {
	if err := validateDownloadURL("http://127.0.0.1/file"); err == nil {
		t.Error("expected error for 127.0.0.1, got nil")
	}
}

func TestValidateDownloadURL_BlocksLoopback127Range(t *testing.T) {
	if err := validateDownloadURL("http://127.255.0.1/file"); err == nil {
		t.Error("expected error for 127.255.0.1, got nil")
	}
}

func TestValidateDownloadURL_BlocksPrivate10(t *testing.T) {
	if err := validateDownloadURL("http://10.0.0.1/file"); err == nil {
		t.Error("expected error for 10.x.x.x, got nil")
	}
}

func TestValidateDownloadURL_BlocksPrivate172_16(t *testing.T) {
	if err := validateDownloadURL("http://172.16.0.1/file"); err == nil {
		t.Error("expected error for 172.16.x.x, got nil")
	}
}

func TestValidateDownloadURL_BlocksPrivate172_31(t *testing.T) {
	if err := validateDownloadURL("http://172.31.255.255/file"); err == nil {
		t.Error("expected error for 172.31.255.255, got nil")
	}
}

func TestValidateDownloadURL_AllowsPublic172_15(t *testing.T) {
	// 172.15.x.x is outside 172.16.0.0/12 — publicly routable.
	if err := validateDownloadURL("http://172.15.0.1/file"); err != nil {
		t.Errorf("expected 172.15.0.1 to be allowed, got: %v", err)
	}
}

func TestValidateDownloadURL_AllowsPublic172_32(t *testing.T) {
	// 172.32.x.x is outside 172.16.0.0/12 — publicly routable.
	if err := validateDownloadURL("http://172.32.0.1/file"); err != nil {
		t.Errorf("expected 172.32.0.1 to be allowed, got: %v", err)
	}
}

func TestValidateDownloadURL_BlocksPrivate192_168(t *testing.T) {
	if err := validateDownloadURL("http://192.168.1.1/file"); err == nil {
		t.Error("expected error for 192.168.x.x, got nil")
	}
}

func TestValidateDownloadURL_BlocksLinkLocal169_254(t *testing.T) {
	// Cloud metadata endpoint
	if err := validateDownloadURL("http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Error("expected error for 169.254.x.x (link-local), got nil")
	}
}

func TestValidateDownloadURL_BlocksUnspecified0_0_0_0(t *testing.T) {
	if err := validateDownloadURL("http://0.0.0.0/file"); err == nil {
		t.Error("expected error for 0.0.0.0, got nil")
	}
}

func TestValidateDownloadURL_BlocksThisNetwork0Range(t *testing.T) {
	// 0.1.2.3 is in the reserved "this" network (0.0.0.0/8).
	if err := validateDownloadURL("http://0.1.2.3/file"); err == nil {
		t.Error("expected error for 0.1.2.3 (0.0.0.0/8), got nil")
	}
}

func TestValidateDownloadURL_BlocksSharedAddressSpace(t *testing.T) {
	// 100.64.0.0/10 is CGNAT shared address space.
	if err := validateDownloadURL("http://100.64.0.1/file"); err == nil {
		t.Error("expected error for 100.64.0.1 (100.64.0.0/10), got nil")
	}
}

func TestValidateDownloadURL_BlocksSharedAddressSpaceUpperBound(t *testing.T) {
	if err := validateDownloadURL("http://100.127.255.255/file"); err == nil {
		t.Error("expected error for 100.127.255.255, got nil")
	}
}

// ── Blocked IPv6 addresses ────────────────────────────────────────────────────

func TestValidateDownloadURL_BlocksIPv6Loopback(t *testing.T) {
	if err := validateDownloadURL("http://[::1]/file"); err == nil {
		t.Error("expected error for ::1 (loopback), got nil")
	}
}

func TestValidateDownloadURL_BlocksIPv6Unspecified(t *testing.T) {
	if err := validateDownloadURL("http://[::]/file"); err == nil {
		t.Error("expected error for :: (unspecified), got nil")
	}
}

func TestValidateDownloadURL_BlocksIPv6UniqueLocal_fc00(t *testing.T) {
	if err := validateDownloadURL("http://[fc00::1]/file"); err == nil {
		t.Error("expected error for fc00::1 (fc00::/7), got nil")
	}
}

func TestValidateDownloadURL_BlocksIPv6UniqueLocal_fd12(t *testing.T) {
	if err := validateDownloadURL("http://[fd12::1]/file"); err == nil {
		t.Error("expected error for fd12::1 (fc00::/7), got nil")
	}
}

func TestValidateDownloadURL_BlocksIPv6LinkLocal_fe80(t *testing.T) {
	if err := validateDownloadURL("http://[fe80::1]/file"); err == nil {
		t.Error("expected error for fe80::1 (fe80::/10 link-local), got nil")
	}
}

// ── IPv4-mapped IPv6 addresses ────────────────────────────────────────────────

func TestValidateDownloadURL_BlocksIPv6MappedLoopback(t *testing.T) {
	if err := validateDownloadURL("http://[::ffff:127.0.0.1]/file"); err == nil {
		t.Error("expected error for ::ffff:127.0.0.1, got nil")
	}
}

func TestValidateDownloadURL_BlocksIPv6MappedPrivate10(t *testing.T) {
	if err := validateDownloadURL("http://[::ffff:10.0.0.1]/file"); err == nil {
		t.Error("expected error for ::ffff:10.0.0.1, got nil")
	}
}

func TestValidateDownloadURL_BlocksIPv6MappedLinkLocal(t *testing.T) {
	if err := validateDownloadURL("http://[::ffff:169.254.169.254]/file"); err == nil {
		t.Error("expected error for ::ffff:169.254.169.254, got nil")
	}
}

func TestValidateDownloadURL_AllowsIPv6MappedPublicIP(t *testing.T) {
	// ::ffff:203.0.113.1 maps to a public IPv4 address — should be allowed.
	if err := validateDownloadURL("http://[::ffff:203.0.113.1]/file"); err != nil {
		t.Errorf("expected ::ffff:203.0.113.1 to be allowed, got: %v", err)
	}
}

// ── SSRF redirect scenarios (validate function wired into CheckRedirect) ──────

// TestDownloadSSRFRedirectBlocked verifies that a redirect target of 127.0.0.1
// is rejected. validateDownloadURL is called by the CheckRedirect hook.
func TestDownloadSSRFRedirectBlocked(t *testing.T) {
	if err := validateDownloadURL("http://127.0.0.1/secret"); err == nil {
		t.Error("expected 127.0.0.1 redirect target to be blocked, got nil")
	}
}

// TestDownloadSSRFPrivateIPv6Blocked verifies that a redirect target of ::1 is rejected.
func TestDownloadSSRFPrivateIPv6Blocked(t *testing.T) {
	if err := validateDownloadURL("http://[::1]:8080/secret"); err == nil {
		t.Error("expected ::1 redirect target to be blocked, got nil")
	}
}

// TestDownloadSSRFPublicRedirectAllowed verifies that public redirect targets pass.
func TestDownloadSSRFPublicRedirectAllowed(t *testing.T) {
	if err := validateDownloadURL("https://example.com/file.bin"); err != nil {
		t.Errorf("expected public URL to be allowed, got: %v", err)
	}
}
