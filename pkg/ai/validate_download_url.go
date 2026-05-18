package ai

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

var netLookupHost = net.LookupHost

// Security: validates pre-fetch and post-redirect URL to prevent SSRF.
//
// validateDownloadURL checks that a URL is safe to download from by rejecting
// private/internal network addresses. Call this both before initiating a request
// and after following any HTTP redirects.
func validateDownloadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("invalid URL: %v", err), err)
	}

	scheme := strings.ToLower(u.Scheme)

	// data: URLs are inline content with no network fetch — no SSRF risk.
	if scheme == "data" {
		return nil
	}

	// Only allow http and https network protocols.
	if scheme != "http" && scheme != "https" {
		return providererrors.NewSSRFError(
			rawURL,
			fmt.Sprintf("URL scheme %q is not allowed: only http, https, and data are permitted", u.Scheme),
			nil,
		)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return providererrors.NewSSRFError(rawURL, "URL must have a hostname", nil)
	}

	// Block localhost and .local/.localhost domain names before DNS resolution.
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
		return providererrors.NewSSRFError(
			rawURL,
			fmt.Sprintf("URL with hostname %q is not allowed", host),
			nil,
		)
	}

	// If host is already an IP, validate it directly.
	if ip := net.ParseIP(host); ip != nil {
		return validateIP(rawURL, ip)
	}
	if ip, ok, err := parseWHATWGIPv4Host(host); ok {
		if err != nil {
			return providererrors.NewSSRFError(rawURL, fmt.Sprintf("invalid IPv4 hostname %q: %v", host, err), err)
		}
		return validateIP(rawURL, ip)
	}

	// Intentional divergence from the TS SDK:
	// resolve hostnames up front so we can reject URLs that map to
	// private/internal addresses before any fetch is attempted.
	addrs, err := netLookupHost(host)
	if err != nil {
		return providererrors.NewSSRFError(
			rawURL,
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

// parseWHATWGIPv4Host handles non-standard IPv4 host forms that JavaScript's
// WHATWG URL parser normalizes before validation, such as 127.1, 2130706433,
// 0177.0.0.1, and 0x7f.0.0.1. net/url leaves these as hostnames, so parse them
// before DNS resolution to preserve SSRF parity with the TypeScript SDK.
func parseWHATWGIPv4Host(host string) (net.IP, bool, error) {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return nil, false, nil
	}
	if len(parts) > 4 {
		if allIPv4NumberParts(parts) {
			return nil, true, fmt.Errorf("too many IPv4 components")
		}
		return nil, false, nil
	}

	values := make([]uint64, len(parts))
	for i, part := range parts {
		if part == "" || !isIPv4NumberPart(part) {
			return nil, false, nil
		}
		value, err := strconv.ParseUint(part, 0, 32)
		if err != nil {
			return nil, true, err
		}
		values[i] = value
	}

	for i := 0; i < len(values)-1; i++ {
		if values[i] > 255 {
			return nil, true, fmt.Errorf("IPv4 component %q exceeds 255", parts[i])
		}
	}

	lastLimit := uint64(1) << uint(8*(5-len(values)))
	if values[len(values)-1] >= lastLimit {
		return nil, true, fmt.Errorf("final IPv4 component %q exceeds %d", parts[len(parts)-1], lastLimit-1)
	}

	var ipValue uint64
	for i := 0; i < len(values)-1; i++ {
		ipValue = (ipValue << 8) + values[i]
	}
	ipValue = (ipValue << uint(8*(5-len(values)))) + values[len(values)-1]

	return net.IPv4(
		byte(ipValue>>24),
		byte(ipValue>>16),
		byte(ipValue>>8),
		byte(ipValue),
	), true, nil
}

func isIPv4NumberPart(part string) bool {
	if strings.HasPrefix(part, "0x") || strings.HasPrefix(part, "0X") {
		if len(part) == 2 {
			return false
		}
		for _, r := range part[2:] {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
		return true
	}
	for _, r := range part {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func allIPv4NumberParts(parts []string) bool {
	for _, part := range parts {
		if !isIPv4NumberPart(part) {
			return false
		}
	}
	return true
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
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("URL resolves to loopback address %s", ip), nil)
	case ip.IsPrivate():
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("URL resolves to private address %s", ip), nil)
	case ip.IsLinkLocalUnicast():
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("URL resolves to link-local address %s", ip), nil)
	case ip.IsLinkLocalMulticast():
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("URL resolves to link-local multicast address %s", ip), nil)
	case ip.IsMulticast():
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("URL resolves to multicast address %s", ip), nil)
	case ip.IsUnspecified():
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("URL resolves to unspecified address %s", ip), nil)
	case sharedAddressSpace.Contains(ip):
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("URL resolves to shared address space (100.64.0.0/10): %s", ip), nil)
	case thisNetwork.Contains(ip):
		return providererrors.NewSSRFError(rawURL, fmt.Sprintf("URL resolves to reserved 'this' network (0.0.0.0/8): %s", ip), nil)
	}

	return nil
}
