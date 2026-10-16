package quiverai

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// whatwgParseURL parses input like the WHATWG URL parser behind JS
// `new URL(input)` (no base URL) and returns the parsed scheme and, for http
// and https, the serialized href (`url.toString()`). ok is false when the
// parser would throw.
//
// Only http and https are parsed in full; any other syntactically valid
// scheme returns ok with an empty href, since callers only need to tell
// "invalid URL" from "valid URL with the wrong protocol".
func whatwgParseURL(input string) (href, scheme string, ok bool) {
	input = strings.TrimFunc(input, func(r rune) bool { return r <= 0x20 })
	input = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(input)

	colon := strings.IndexByte(input, ':')
	if colon <= 0 || !isASCIIAlpha(input[0]) {
		return "", "", false
	}
	for i := 1; i < colon; i++ {
		c := input[i]
		if !isASCIIAlpha(c) && (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return "", "", false
		}
	}
	scheme = strings.ToLower(input[:colon])
	if scheme != "http" && scheme != "https" {
		return "", scheme, true
	}
	rest := strings.TrimLeft(input[colon+1:], `/\`)

	end := strings.IndexAny(rest, `/\?#`)
	if end == -1 {
		end = len(rest)
	}
	authority, remaining := rest[:end], rest[end:]

	var userinfo string
	if at := strings.LastIndexByte(authority, '@'); at != -1 {
		userinfo, authority = authority[:at], authority[at+1:]
		if authority == "" {
			return "", scheme, false
		}
	}

	hostPart, portPart := authority, ""
	if strings.HasPrefix(authority, "[") {
		closing := strings.IndexByte(authority, ']')
		if closing == -1 {
			return "", scheme, false
		}
		hostPart = authority[:closing+1]
		if tail := authority[closing+1:]; tail != "" {
			if tail[0] != ':' {
				return "", scheme, false
			}
			portPart = tail[1:]
		}
	} else if colon := strings.IndexByte(authority, ':'); colon != -1 {
		hostPart, portPart = authority[:colon], authority[colon+1:]
	}

	port := ""
	if portPart != "" {
		for i := 0; i < len(portPart); i++ {
			if portPart[i] < '0' || portPart[i] > '9' {
				return "", scheme, false
			}
		}
		trimmed := strings.TrimLeft(portPart, "0")
		n := 0
		if trimmed != "" {
			if len(trimmed) > 5 {
				return "", scheme, false
			}
			n, _ = strconv.Atoi(trimmed)
		}
		if n > 65535 {
			return "", scheme, false
		}
		if (scheme != "http" || n != 80) && (scheme != "https" || n != 443) {
			port = strconv.Itoa(n)
		}
	}

	host, hostOK := whatwgParseHost(hostPart)
	if !hostOK {
		return "", scheme, false
	}

	var b strings.Builder
	b.WriteString(scheme)
	b.WriteString("://")
	if userinfo != "" {
		user, pass, _ := strings.Cut(userinfo, ":")
		b.WriteString(whatwgPercentEncode(user, whatwgUserinfoSet))
		if pass != "" {
			b.WriteByte(':')
			b.WriteString(whatwgPercentEncode(pass, whatwgUserinfoSet))
		}
		if user != "" || pass != "" {
			b.WriteByte('@')
		}
	}
	b.WriteString(host)
	if port != "" {
		b.WriteByte(':')
		b.WriteString(port)
	}

	fragment, hasFragment := "", false
	if i := strings.IndexByte(remaining, '#'); i != -1 {
		remaining, fragment, hasFragment = remaining[:i], remaining[i+1:], true
	}
	query, hasQuery := "", false
	if i := strings.IndexByte(remaining, '?'); i != -1 {
		remaining, query, hasQuery = remaining[:i], remaining[i+1:], true
	}
	b.WriteString(whatwgPath(remaining))
	if hasQuery {
		b.WriteByte('?')
		b.WriteString(whatwgPercentEncode(query, whatwgSpecialQuerySet))
	}
	if hasFragment {
		b.WriteByte('#')
		b.WriteString(whatwgPercentEncode(fragment, whatwgFragmentSet))
	}
	return b.String(), scheme, true
}

func isASCIIAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// WHATWG percent-encode sets (beyond C0 controls, space and non-ASCII, which
// are always encoded).
const (
	whatwgFragmentSet     = "\"<>`"
	whatwgSpecialQuerySet = "\"#<>'"
	whatwgPathSet         = "\"#<>?^`{}"
	whatwgUserinfoSet     = whatwgPathSet + "/:;=@[\\]|"
)

func whatwgPercentEncode(s, set string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || c >= 0x7F || strings.IndexByte(set, c) != -1 {
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// whatwgPath serializes a special-scheme path: backslashes are slashes, dot
// segments are resolved, and each segment is percent-encoded. An empty path
// is "/".
func whatwgPath(raw string) string {
	raw = strings.ReplaceAll(raw, `\`, "/")
	raw = strings.TrimPrefix(raw, "/")
	segments := strings.Split(raw, "/")
	var path []string
	for i, segment := range segments {
		last := i == len(segments)-1
		switch strings.ToLower(segment) {
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
			if last {
				path = append(path, "")
			}
		case ".", "%2e":
			if last {
				path = append(path, "")
			}
		default:
			path = append(path, whatwgPercentEncode(segment, whatwgPathSet))
		}
	}
	return "/" + strings.Join(path, "/")
}

var whatwgIDNA = idna.New(
	idna.MapForLookup(),
	idna.Transitional(false),
	idna.BidiRule(),
	idna.CheckJoiners(true),
	idna.StrictDomainName(false),
	idna.CheckHyphens(false),
	idna.VerifyDNSLength(false),
)

// whatwgParseHost is the WHATWG host parser for a special scheme.
func whatwgParseHost(host string) (string, bool) {
	if strings.HasPrefix(host, "[") {
		if !strings.HasSuffix(host, "]") {
			return "", false
		}
		addr, ok := whatwgParseIPv6(host[1 : len(host)-1])
		if !ok {
			return "", false
		}
		return "[" + addr + "]", true
	}

	decoded := whatwgPercentDecode(host)
	if !utf8.ValidString(decoded) {
		decoded = strings.ToValidUTF8(decoded, "�")
	}
	ascii, err := whatwgIDNA.ToASCII(decoded)
	if err != nil || ascii == "" {
		return "", false
	}
	for i := 0; i < len(ascii); i++ {
		c := ascii[i]
		if c <= 0x20 || c == 0x7F || strings.IndexByte("#/:<>?@[\\]^|%", c) != -1 {
			return "", false
		}
	}
	if whatwgEndsInANumber(ascii) {
		return whatwgParseIPv4(ascii)
	}
	return ascii, true
}

func whatwgPercentDecode(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s)+0 && isHex(s[i+1]) && isHex(s[i+2]) {
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b = append(b, byte(v))
			i += 2
			continue
		}
		b = append(b, s[i])
	}
	return string(b)
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func whatwgEndsInANumber(host string) bool {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := whatwgParseIPv4Number(last)
	return ok
}

// whatwgParseIPv4Number is the WHATWG IPv4 number parser (decimal, 0x hex,
// leading-0 octal).
func whatwgParseIPv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	radix := 10
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s, radix = s[2:], 16
	} else if len(s) >= 2 && s[0] == '0' {
		s, radix = s[1:], 8
	}
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(s, radix, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return ^uint64(0), true
		}
		return 0, false
	}
	return n, true
}

func whatwgParseIPv4(host string) (string, bool) {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return "", false
	}
	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		n, ok := whatwgParseIPv4Number(part)
		if !ok {
			return "", false
		}
		numbers[i] = n
	}
	for _, n := range numbers[:len(numbers)-1] {
		if n > 255 {
			return "", false
		}
	}
	limit := uint64(1) << (8 * uint(5-len(numbers)))
	if last := numbers[len(numbers)-1]; last >= limit {
		return "", false
	}
	ipv4 := numbers[len(numbers)-1]
	for i, n := range numbers[:len(numbers)-1] {
		ipv4 += n << (8 * uint(3-i))
	}
	return fmt.Sprintf("%d.%d.%d.%d", ipv4>>24&255, ipv4>>16&255, ipv4>>8&255, ipv4&255), true
}

// whatwgParseIPv6 parses and serializes (compressed, lower case) an IPv6
// address.
func whatwgParseIPv6(input string) (string, bool) {
	var address [8]uint16
	piece, compress := 0, -1
	p := 0
	at := func(i int) byte {
		if i >= 0 && i < len(input) {
			return input[i]
		}
		return 0
	}
	if at(p) == ':' {
		if at(p+1) != ':' {
			return "", false
		}
		p += 2
		piece++
		compress = piece
	}
	for p < len(input) {
		if piece == 8 {
			return "", false
		}
		if at(p) == ':' {
			if compress != -1 {
				return "", false
			}
			p++
			piece++
			compress = piece
			continue
		}
		value, length := 0, 0
		for length < 4 && p < len(input) && isHex(input[p]) {
			v, _ := strconv.ParseUint(string(input[p]), 16, 8)
			value = value*16 + int(v)
			p++
			length++
		}
		if at(p) == '.' {
			if length == 0 {
				return "", false
			}
			p -= length
			if piece > 6 {
				return "", false
			}
			seen := 0
			for p < len(input) {
				v4 := -1
				if seen > 0 {
					if at(p) == '.' && seen < 4 {
						p++
					} else {
						return "", false
					}
				}
				if p >= len(input) || input[p] < '0' || input[p] > '9' {
					return "", false
				}
				for p < len(input) && input[p] >= '0' && input[p] <= '9' {
					n := int(input[p] - '0')
					switch v4 {
					case -1:
						v4 = n
					case 0:
						return "", false
					default:
						v4 = v4*10 + n
					}
					if v4 > 255 {
						return "", false
					}
					p++
				}
				address[piece] = address[piece]*0x100 + uint16(v4)
				seen++
				if seen == 2 || seen == 4 {
					piece++
				}
			}
			if seen != 4 {
				return "", false
			}
			break
		} else if at(p) == ':' {
			p++
			if p >= len(input) {
				return "", false
			}
		} else if p < len(input) {
			return "", false
		}
		address[piece] = uint16(value)
		piece++
	}
	if compress != -1 {
		swaps := piece - compress
		piece = 7
		for piece != 0 && swaps > 0 {
			address[piece], address[compress+swaps-1] = address[compress+swaps-1], address[piece]
			piece--
			swaps--
		}
	} else if piece != 8 {
		return "", false
	}

	// Serialize: compress the first longest run of two or more zero pieces.
	bestStart, bestLen := -1, 0
	for i := 0; i < 8; {
		if address[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && address[j] == 0 {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	if bestLen < 2 {
		bestStart = -1
	}
	var b strings.Builder
	ignore0 := false
	for i := 0; i < 8; i++ {
		if ignore0 && address[i] == 0 {
			continue
		}
		ignore0 = false
		if bestStart == i {
			if i == 0 {
				b.WriteString("::")
			} else {
				b.WriteString(":")
			}
			ignore0 = true
			continue
		}
		b.WriteString(strconv.FormatUint(uint64(address[i]), 16))
		if i != 7 {
			b.WriteByte(':')
		}
	}
	return b.String(), true
}
