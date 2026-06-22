package fileutil

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

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
	host := strings.ToLower(strings.TrimRight(parsed.Hostname(), "."))
	if host == "" {
		return providererrors.NewDownloadError(raw, 0, "", "URL must have a hostname", nil)
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
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
	if len(parts) == 0 || len(parts) > 4 {
		return nil, "", false, numericLikeHost(host)
	}
	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		n, ok := parseWHATWGIPv4Number(part)
		if !ok {
			if numericLikeHost(host) {
				return nil, "", false, true
			}
			return nil, "", false, false
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

func numericLikeHost(host string) bool {
	if host == "" {
		return false
	}
	for _, part := range strings.Split(host, ".") {
		if !numericLikeIPv4Part(part) {
			return false
		}
	}
	return true
}

func numericLikeIPv4Part(part string) bool {
	if part == "" {
		return true
	}
	if len(part) >= 2 && part[0] == '0' && (part[1] == 'x' || part[1] == 'X') {
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
			(a == 192 && b == 168) ||
			(a == 198 && (b == 18 || b == 19)) ||
			a >= 240
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
