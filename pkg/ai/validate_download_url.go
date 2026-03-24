package ai

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Security: validates pre-fetch and post-redirect URL to prevent SSRF.
//
// validateDownloadURL checks that a URL is safe to download from by rejecting
// private/internal network addresses. Call this both before initiating a request
// and after following any HTTP redirects.
func validateDownloadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("invalid URL: %v", err), err)
	}

	// data: URLs are inline content with no network fetch — no SSRF risk.
	if u.Scheme == "data" {
		return nil
	}

	// Only allow http and https network protocols.
	if u.Scheme != "http" && u.Scheme != "https" {
		return providererrors.NewDownloadError(
			rawURL, 0, "",
			fmt.Sprintf("URL scheme %q is not allowed: only http, https, and data are permitted", u.Scheme),
			nil,
		)
	}

	host := u.Hostname()
	if host == "" {
		return providererrors.NewDownloadError(rawURL, 0, "", "URL must have a hostname", nil)
	}

	// Block localhost and .local/.localhost domain names before DNS resolution.
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
		return providererrors.NewDownloadError(
			rawURL, 0, "",
			fmt.Sprintf("URL with hostname %q is not allowed", host),
			nil,
		)
	}

	// If host is already an IP, validate it directly.
	if ip := net.ParseIP(host); ip != nil {
		return validateIP(rawURL, ip)
	}

	// Resolve hostname to IPs and validate each one.
	addrs, err := net.LookupHost(host)
	if err != nil {
		return providererrors.NewDownloadError(
			rawURL, 0, "",
			fmt.Sprintf("failed to resolve hostname %q: %v", host, err),
			err,
		)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if err := validateIP(rawURL, ip); err != nil {
			return err
		}
	}

	return nil
}

// sharedAddressSpace is CGNAT 100.64.0.0/10, not covered by net.IP.IsPrivate().
var sharedAddressSpace = func() *net.IPNet {
	_, ipnet, _ := net.ParseCIDR("100.64.0.0/10")
	return ipnet
}()

// thisNetwork is the reserved "this" network 0.0.0.0/8 (RFC 1122 §3.2.1.3).
// net.IP.IsUnspecified() only covers 0.0.0.0 itself; we block the whole block.
var thisNetwork = func() *net.IPNet {
	_, ipnet, _ := net.ParseCIDR("0.0.0.0/8")
	return ipnet
}()

// validateIP checks a single IP address for SSRF-unsafe ranges.
func validateIP(rawURL string, ip net.IP) error {
	// Unwrap IPv6-mapped IPv4 (e.g. ::ffff:192.168.1.1) so the IPv4 checks apply.
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	switch {
	case ip.IsLoopback():
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("URL resolves to loopback address %s", ip), nil)
	case ip.IsPrivate():
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("URL resolves to private address %s", ip), nil)
	case ip.IsLinkLocalUnicast():
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("URL resolves to link-local address %s", ip), nil)
	case ip.IsLinkLocalMulticast():
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("URL resolves to link-local multicast address %s", ip), nil)
	case ip.IsMulticast():
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("URL resolves to multicast address %s", ip), nil)
	case ip.IsUnspecified():
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("URL resolves to unspecified address %s", ip), nil)
	case sharedAddressSpace.Contains(ip):
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("URL resolves to shared address space (100.64.0.0/10): %s", ip), nil)
	case thisNetwork.Contains(ip):
		return providererrors.NewDownloadError(rawURL, 0, "", fmt.Sprintf("URL resolves to reserved 'this' network (0.0.0.0/8): %s", ip), nil)
	}

	return nil
}

