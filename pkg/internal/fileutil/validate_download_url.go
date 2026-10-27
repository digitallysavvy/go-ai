package fileutil

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// ValidateDownloadURL rejects URL forms that can bypass SSRF protections.
func ValidateDownloadURL(raw string) error {
	normalizedRaw, normalizeErr := normalizeEscapedURLHost(raw)
	if normalizeErr != nil {
		return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("Invalid URL: %s", raw), normalizeErr)
	}
	parsed, err := url.Parse(normalizedRaw)
	if err != nil || parsed.Scheme == "" {
		return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("Invalid URL: %s", raw), err)
	}
	if parsed.Scheme == "data" {
		return nil
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("URL scheme must be http, https, or data, got %s:", parsed.Scheme), nil)
	}
	if err := validateURLPort(parsed); err != nil {
		return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("Invalid URL: %s", raw), err)
	}
	host := NormalizeHostname(parsed.Hostname())
	if host == "" {
		return providererrors.NewDownloadError(raw, 0, "", "URL must have a hostname", nil)
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") {
		return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("URL with hostname %s is not allowed", host), nil)
	}
	if strings.HasPrefix(parsed.Host, "[") && strings.Contains(parsed.Host, "]") {
		ip := net.ParseIP(host)
		if ip == nil {
			return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("Invalid URL: %s", raw), nil)
		}
		if isBlockedIP(ip) {
			return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("URL with IPv6 address %s is not allowed", canonicalIPv6Hostname(ip)), nil)
		}
	}
	if ip, display, invalid := parseLiteralIP(host); invalid {
		return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("Invalid URL: %s", raw), nil)
	} else if ip != nil && isBlockedIP(ip) {
		return providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("URL with IP address %s is not allowed", display), nil)
	}
	return nil
}

// NormalizeEscapedURLHost percent-decodes the host of an absolute URL the way
// the WHATWG URL parser does (net/url rejects such hosts), so that
// "http://%6c%6f%63%61%6c%68%6f%73%74/" is seen as "http://localhost/". It
// returns an error when the decoded host holds a forbidden host code point.
func NormalizeEscapedURLHost(raw string) (string, error) { return normalizeEscapedURLHost(raw) }

func normalizeEscapedURLHost(raw string) (string, error) {
	schemeEnd := strings.Index(raw, "://")
	if schemeEnd < 0 {
		return raw, nil
	}
	authorityStart := schemeEnd + len("://")
	authorityEnd := len(raw)
	if i := strings.IndexAny(raw[authorityStart:], "/?#"); i >= 0 {
		authorityEnd = authorityStart + i
	}
	authority := raw[authorityStart:authorityEnd]
	if !strings.Contains(authority, "%") {
		return raw, nil
	}

	userinfo := ""
	hostport := authority
	if at := strings.LastIndex(hostport, "@"); at >= 0 {
		userinfo = hostport[:at+1]
		hostport = hostport[at+1:]
	}
	if strings.HasPrefix(hostport, "[") {
		return raw, nil
	}

	host := hostport
	port := ""
	if colon := strings.LastIndex(hostport, ":"); colon >= 0 && strings.Count(hostport, ":") == 1 {
		suffix := hostport[colon+1:]
		if suffix == "" || allASCIIAlphaDigits(suffix) {
			host = hostport[:colon]
			port = hostport[colon:]
		}
	}
	if !strings.Contains(host, "%") {
		return raw, nil
	}
	decoded, err := url.PathUnescape(host)
	if err != nil || hasInvalidEscapedHostRune(decoded) {
		if err == nil {
			err = fmt.Errorf("invalid escaped host")
		}
		return "", err
	}
	normalizedAuthority := userinfo + decoded + port
	return raw[:authorityStart] + normalizedAuthority + raw[authorityEnd:], nil
}

func allASCIIAlphaDigits(text string) bool {
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hasInvalidEscapedHostRune(host string) bool {
	for _, r := range host {
		if r <= 0x20 || r == 0x7f {
			return true
		}
		switch r {
		case '/', '\\', ':', '@', '?', '#', '[', ']':
			return true
		}
	}
	return false
}

func validateURLPort(parsed *url.URL) error {
	port := parsed.Port()
	if port == "" {
		return nil
	}
	n, err := strconv.ParseUint(port, 10, 32)
	if err != nil || n > 65535 {
		return fmt.Errorf("invalid port")
	}
	return nil
}

func parseLiteralIP(host string) (net.IP, string, bool) {
	if ip, display, ok, invalid := parseWHATWGIPv4Host(host); ok || invalid {
		return ip, display, invalid
	}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return ip, v4.String(), false
		}
		return ip, canonicalIPv6Hostname(ip), false
	}
	return nil, "", false
}

func parseWHATWGIPv4Host(host string) (net.IP, string, bool, bool) {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	// WHATWG "ends in a number": only such hosts go through the IPv4 parser,
	// and for them every parse failure makes the URL invalid ("foo.1",
	// "1.2.3.4.5" and "09.1" throw in TS new URL).
	if strings.Contains(host, ":") || len(parts) == 0 || !endsInIPv4Number(parts[len(parts)-1]) { // IPv6 literals are not IPv4 hosts
		return nil, "", false, false
	}
	if len(parts) > 4 {
		return nil, "", false, true
	}
	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		n, ok := parseWHATWGIPv4Number(part)
		if !ok {
			return nil, "", false, true
		}
		numbers[i] = n
	}
	for i := 0; i < len(numbers)-1; i++ {
		if numbers[i] > 255 {
			return nil, "", false, true
		}
	}
	lastLimit := uint64(1) << (8 * (5 - len(numbers)))
	if numbers[len(numbers)-1] >= lastLimit {
		return nil, "", false, true
	}
	ipv4 := numbers[len(numbers)-1]
	for i := 0; i < len(numbers)-1; i++ {
		ipv4 += numbers[i] << (8 * (3 - i))
	}
	ip := net.IPv4(byte(ipv4>>24), byte(ipv4>>16), byte(ipv4>>8), byte(ipv4))
	return ip, ip.String(), true, false
}

func parseWHATWGIPv4Number(part string) (uint64, bool) {
	if part == "" {
		return 0, false
	}
	base := 10
	text := part
	if len(text) >= 2 && text[0] == '0' && (text[1] == 'x' || text[1] == 'X') {
		base = 16
		text = text[2:]
	} else if len(text) >= 2 && text[0] == '0' {
		base = 8
		text = text[1:]
	}
	if text == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(text, base, 64)
	return n, err == nil
}

// endsInIPv4Number is the WHATWG "ends in a number" test on the last host
// label: all ASCII digits, or "0x"/"0X" followed by hex digits only.
func endsInIPv4Number(part string) bool {
	if part == "" {
		return false
	}
	if len(part) >= 2 && part[0] == '0' && (part[1] == 'x' || part[1] == 'X') {
		for _, r := range part[2:] {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
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

// CanonicalIPHost returns the WHATWG serialization of an IP host: IPv4 in
// any form the URL parser accepts ("127.1", "0x7f.0.0.1", "2130706433")
// becomes dotted decimal, and a bracketed IPv6 literal is compressed and
// lowercased. ok is false for a domain name; invalid is true for a host that
// ends in a number but is not a valid IPv4 address, which TS new URL rejects.
func CanonicalIPHost(host string) (canonical string, ok bool, invalid bool) {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		ip := net.ParseIP(host[1 : len(host)-1])
		if ip == nil || strings.Contains(host, "%") {
			return "", false, true
		}
		return canonicalIPv6Hostname(ip), true, false
	}
	_, display, isIPv4, invalid := parseWHATWGIPv4Host(host)
	if invalid {
		return "", false, true
	}
	return display, isIPv4, false
}

// IsBlockedIP reports whether ip is in a private, loopback, link-local,
// multicast, reserved, documentation or otherwise non-public range, mirroring
// the TypeScript SDK's isPrivateIPv4 / isPrivateIPv6 checks in
// provider-utils validate-download-url.ts.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return isBlockedIP(ip)
}

// ValidateDownloadAddress validates an address returned by DNS before it is
// used to open a socket (TS validateDownloadAddress). hostname is the name
// that was resolved and is used in the error.
func ValidateDownloadAddress(hostname string, ip net.IP) error {
	if ip == nil || isBlockedIP(ip) {
		address := "<invalid>"
		if ip != nil {
			address = ip.String()
		}
		return providererrors.NewDownloadError(hostname, 0, "", fmt.Sprintf("Hostname %s resolved to disallowed IP address %s", hostname, address), nil)
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		a, b, c := v4[0], v4[1], v4[2]
		return a == 0 ||
			a == 10 ||
			(a == 100 && b >= 64 && b <= 127) ||
			a == 127 ||
			(a == 169 && b == 254) ||
			(a == 172 && b >= 16 && b <= 31) ||
			(a == 192 && b == 0 && c == 0) ||
			(a == 192 && b == 0 && c == 2) || // TEST-NET-1
			(a == 192 && b == 168) ||
			(a == 198 && (b == 18 || b == 19)) ||
			(a == 198 && b == 51 && c == 100) || // TEST-NET-2
			(a == 203 && b == 0 && c == 113) || // TEST-NET-3
			a >= 224 // 224.0.0.0/4 multicast and 240.0.0.0/4 reserved
	}
	if v6 := ip.To16(); v6 != nil {
		// 2001:db8::/32 (documentation range, not globally routable)
		if v6[0] == 0x20 && v6[1] == 0x01 && v6[2] == 0x0d && v6[3] == 0xb8 {
			return true
		}
		// 3fff::/20 (documentation range, RFC 9637)
		if v6[0] == 0x3f && v6[1] == 0xff && v6[2]&0xf0 == 0 {
			return true
		}
	}
	if v6 := ip.To16(); v6 != nil && hasEmbeddedBlockedIPv4(v6) {
		return true
	}
	if ip := ip.To16(); ip != nil && ip[0] == 0xfe && ip[1]&0xc0 == 0xc0 {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsUnspecified() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func hasEmbeddedBlockedIPv4(ip net.IP) bool {
	if len(ip) != net.IPv6len {
		return false
	}
	embedded := net.IPv4(ip[12], ip[13], ip[14], ip[15])
	allZeroPrefix := true
	for i := 0; i < 12; i++ {
		if ip[i] != 0 {
			allZeroPrefix = false
			break
		}
	}
	if allZeroPrefix && isBlockedIP(embedded) {
		return true
	}
	if zeroBytes(ip[0:10]) && ip[10] == 0xff && ip[11] == 0xff && isBlockedIP(embedded) {
		return true
	}
	if zeroBytes(ip[0:8]) && ip[8] == 0xff && ip[9] == 0xff && ip[10] == 0 && ip[11] == 0 && isBlockedIP(embedded) {
		return true
	}
	if ip[0] == 0 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
		if zeroBytes(ip[4:12]) && isBlockedIP(embedded) {
			return true
		}
		if ip[4] == 0 && ip[5] == 1 && isBlockedIP(embedded) {
			return true
		}
	}
	return false
}

func zeroBytes(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

func canonicalIPv6Hostname(ip net.IP) string {
	v6 := ip.To16()
	if v6 == nil {
		return "[" + strings.ToLower(ip.String()) + "]"
	}
	groups := make([]uint16, 8)
	for i := 0; i < 8; i++ {
		groups[i] = uint16(v6[i*2])<<8 | uint16(v6[i*2+1])
	}

	bestStart, bestLen := -1, 0
	for i := 0; i < len(groups); {
		if groups[i] != 0 {
			i++
			continue
		}
		start := i
		for i < len(groups) && groups[i] == 0 {
			i++
		}
		if length := i - start; length > bestLen && length > 1 {
			bestStart, bestLen = start, length
		}
	}

	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < len(groups); i++ {
		if i == bestStart {
			b.WriteString("::")
			i += bestLen - 1
			continue
		}
		if i > 0 && i != bestStart+bestLen {
			b.WriteByte(':')
		}
		b.WriteString(strconv.FormatUint(uint64(groups[i]), 16))
	}
	b.WriteByte(']')
	return b.String()
}

// NormalizeHostname lowercases host, maps it through IDNA (UTS #46, as the
// WHATWG URL parser and net/http do before dialing) and strips trailing dots,
// so that fullwidth letters, ideographic full stops and trailing dots cannot
// slip past string-based hostname blocklists. Hosts that fail IDNA mapping
// are returned lowercased; net/http rejects them at dial time.
func NormalizeHostname(host string) string {
	host = strings.ToLower(host)
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		host = ascii
	}
	return strings.TrimRight(strings.ToLower(host), ".")
}
