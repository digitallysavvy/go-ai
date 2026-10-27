package toolwebfetch

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// canonicalURL parses an absolute http(s) URL and returns it in a canonical
// form without the fragment (WHATWG `new URL(url)` with `hash = ”`): leading
// and trailing C0 controls and spaces trimmed, scheme and host lowercased, IDN
// hosts in punycode, IPv4 hosts in dotted decimal, the port in decimal with
// the default port removed, dot segments removed from the path, and path and
// query percent-encoded with the WHATWG sets. ok is false when TS new URL
// would throw.
func canonicalURL(raw string) (*url.URL, bool) {
	raw = strings.TrimFunc(raw, func(r rune) bool { return r <= 0x20 })
	preprocessed := whatwgPreprocess(raw)
	special := isSpecialScheme(preprocessed)
	var path, query string
	if special {
		var ok bool
		preprocessed, path, query, ok = splitSpecialURL(preprocessed)
		if !ok {
			return nil, false
		}
	}
	// The WHATWG parser percent-decodes the host; net/url rejects it.
	normalized, err := fileutil.NormalizeEscapedURLHost(preprocessed)
	if err != nil {
		return nil, false
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme == "" {
		return nil, false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Fragment = ""
	parsed.RawFragment = ""
	// WHATWG drops empty credentials ("https://@host/" has none).
	if parsed.User != nil {
		if password, _ := parsed.User.Password(); parsed.User.Username() == "" && password == "" {
			parsed.User = nil
		}
	}
	if !special {
		return parsed, true
	}
	if parsed.Host == "" {
		return nil, false
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.Contains(host, ":") { // IPv6 literal
		canon, ok, _ := fileutil.CanonicalIPHost("[" + host + "]")
		if !ok {
			return nil, false
		}
		host = canon
	} else {
		if ascii := fileutilASCIIHost(host); ascii != "" {
			host = ascii
		}
		if canon, ok, invalid := fileutil.CanonicalIPHost(host); invalid {
			return nil, false
		} else if ok {
			host = canon
		}
	}
	port, ok := canonicalPort(parsed.Scheme, parsed.Port())
	if !ok {
		return nil, false
	}
	if port != "" {
		host += ":" + port
	}
	parsed.Host = host
	parsed.Path = unescapeLenient(path)
	parsed.RawPath = path
	parsed.RawQuery = query
	return parsed, true
}

// isSpecialScheme reports an http: or https: URL.
func isSpecialScheme(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http:") || strings.HasPrefix(lower, "https:")
}

// splitSpecialURL rewrites an http(s) URL for net/url and returns its
// WHATWG path and query. Like the WHATWG parser it accepts any number of
// slashes after the scheme, removes dot segments ("." and "..", also
// percent-encoded) and percent-encodes the path and query sets. A "%" that
// does not start an escape is kept in the returned path but escaped in the
// rewritten URL, which net/url would otherwise reject.
func splitSpecialURL(raw string) (rewritten, path, query string, ok bool) {
	colon := strings.IndexByte(raw, ':')
	scheme, rest := raw[:colon], strings.TrimLeft(raw[colon+1:], "/")
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest = rest[:i]
	}
	authorityEnd := len(rest)
	if i := strings.IndexAny(rest, "/?"); i >= 0 {
		authorityEnd = i
	}
	authority, rest := rest[:authorityEnd], rest[authorityEnd:]
	if authority == "" {
		return "", "", "", false
	}
	rawPath, rawQuery, hasQuery := rest, "", false
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rawPath, rawQuery, hasQuery = rest[:i], rest[i+1:], true
	}
	path = removeDotSegments(percentEncode(rawPath, inPathSet))
	rewritten = scheme + "://" + authority + escapeStrayPercents(path)
	if hasQuery {
		query = percentEncode(rawQuery, inSpecialQuerySet)
		rewritten += "?" + escapeStrayPercents(query)
	}
	return rewritten, path, query, true
}

// inPathSet is the WHATWG path percent-encode set (C0 controls, space and
// anything above "~" are handled by percentEncode).
func inPathSet(c byte) bool { return strings.IndexByte("\"#<>?^`{}", c) >= 0 }

// inSpecialQuerySet is the WHATWG special-query percent-encode set.
func inSpecialQuerySet(c byte) bool { return strings.IndexByte("\"#<>'", c) >= 0 }

// inFragmentSet is the WHATWG fragment percent-encode set.
func inFragmentSet(c byte) bool { return strings.IndexByte("\"<>`", c) >= 0 }

func percentEncode(s string, inSet func(byte) bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || c > 0x7e || inSet(c) {
			b.WriteString("%" + strings.ToUpper(strconv.FormatUint(uint64(c)>>4, 16)) + strings.ToUpper(strconv.FormatUint(uint64(c)&0xf, 16)))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// removeDotSegments applies the WHATWG path parser's handling of single-dot
// and double-dot segments to an absolute path ("" means "/").
func removeDotSegments(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	out := make([]string, 0, len(segments))
	for i, segment := range segments {
		last := i == len(segments)-1
		switch strings.ToLower(segment) {
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		case ".", "%2e":
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, segment)
		}
	}
	return "/" + strings.Join(out, "/")
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// escapeStrayPercents escapes each "%" that is not followed by two hex digits.
func escapeStrayPercents(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && (i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2])) {
			b.WriteString("%25")
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// unescapeLenient decodes valid escapes and keeps stray "%" as is.
func unescapeLenient(s string) string {
	decoded, err := url.PathUnescape(escapeStrayPercents(s))
	if err != nil {
		return s
	}
	return decoded
}

// canonicalPort returns the port in decimal, or "" for none or the scheme's
// default. ok is false for a port TS new URL rejects.
func canonicalPort(scheme, port string) (string, bool) {
	if port == "" {
		return "", true
	}
	n, err := strconv.ParseUint(port, 10, 32)
	if err != nil || n > 65535 {
		return "", false
	}
	if (scheme == "http" && n == 80) || (scheme == "https" && n == 443) {
		return "", true
	}
	return strconv.FormatUint(n, 10), true
}

// fileutilASCIIHost maps a host through IDNA, keeping a trailing dot.
func fileutilASCIIHost(host string) string {
	trimmed := strings.TrimRight(host, ".")
	dots := host[len(trimmed):]
	ascii := fileutil.NormalizeHostname(trimmed)
	if ascii == "" {
		return ""
	}
	return ascii + dots
}

// whatwgPreprocess applies the parts of the WHATWG URL parser that net/url
// does not: ASCII tabs and newlines are removed everywhere, and for http(s)
// URLs, and relative references (always resolved against an http(s) URL
// here), a backslash before the query or fragment is a slash.
func whatwgPreprocess(raw string) string {
	raw = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(raw)
	if hasURLScheme(raw) && !isSpecialScheme(raw) {
		return raw
	}
	end := len(raw)
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		end = i
	}
	return strings.ReplaceAll(raw[:end], "\\", "/") + raw[end:]
}

// hasURLScheme reports a leading WHATWG scheme: an ASCII letter followed by
// letters, digits, "+", "-" or "." and then ":".
func hasURLScheme(raw string) bool {
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		case i > 0 && c == ':':
			return true
		default:
			return false
		}
	}
	return false
}
