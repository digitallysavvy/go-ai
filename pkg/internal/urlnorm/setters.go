package urlnorm

import (
	"fmt"
	"strings"
)

// SetProtocol is the `URL.prototype.protocol` setter: value is a scheme with
// or without the trailing ':' ("wss" or "wss:"). As in the URL Standard
// (scheme state with a state override), the URL is left unchanged when value
// is not a valid scheme, when it would switch between a special and a
// non-special scheme, when it would make a URL with credentials or a port a
// file: URL, or when the URL is a file: URL with an empty host. A port equal
// to the new scheme's default port becomes null.
func (u *URL) SetProtocol(value string) {
	value = removeTabNewline(value) // the basic URL parser strips them first
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := rune(value[i])
		if i == 0 {
			if !isASCIIAlpha(c) {
				return
			}
			b.WriteRune(toLowerASCII(c))
			continue
		}
		if isASCIIAlphanumeric(c) || c == '+' || c == '-' || c == '.' {
			b.WriteRune(toLowerASCII(c))
			continue
		}
		if c == ':' {
			break
		}
		return
	}
	scheme := b.String()
	if scheme == "" || isSpecialScheme(u.Scheme) != isSpecialScheme(scheme) {
		return
	}
	if (u.Username != "" || u.Password != "" || u.Port >= 0) && scheme == "file" {
		return
	}
	if u.Scheme == "file" && u.HasHost && u.Host == "" {
		return
	}
	u.Scheme = scheme
	if port, ok := specialSchemes[scheme]; ok && port >= 0 && u.Port == port {
		u.Port = -1
	}
}

// SetPathname is the `URL.prototype.pathname` setter: the path is replaced
// by value parsed in the path start state (dot segments resolved, characters
// outside the path percent-encode set encoded; "?" and "#" are part of the
// path). The query and fragment are kept. A URL with an opaque path is left
// unchanged.
func (u *URL) SetPathname(value string) {
	if u.opaque {
		return
	}
	var v strings.Builder
	for _, r := range value {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			// removed, as by the basic URL parser
		case r == '?' || r == '#' || r == ' ' || r < 0x20:
			// The state override keeps "?" and "#" in the path; leading and
			// trailing C0 controls and spaces are not trimmed.
			fmt.Fprintf(&v, "%%%02X", r)
		default:
			v.WriteRune(r)
		}
	}
	p := v.String()
	if u.IsSpecial() {
		if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, `\`) {
			p = "/" + p
		}
	} else if u.HasHost {
		if p != "" && !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
	} else {
		// Path start state with a null host: a missing leading "/" is
		// implied ("foo:/a" pathname "c" is "foo:/c", "" is "foo:/").
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if strings.HasPrefix(p, "//") {
			p = "/." + p // keep "//" in the path rather than read it as an authority
		}
	}
	parsed, err := Parse(u.authorityPrefix() + p)
	if err != nil || parsed.opaque {
		return
	}
	u.path = parsed.path
}

// ResolvePath is `new URL(path, u)` for a path-absolute or scheme-relative
// reference: path starts with "/" (or "\" for a special URL). A
// path-absolute reference keeps u's scheme, credentials, host and port and
// takes its path, query and fragment from path; a scheme-relative one
// ("//host/x", tabs and newlines dropped first) keeps only u's scheme. It
// fails with ErrInvalidURL when u has an opaque path or path starts with
// neither.
func (u *URL) ResolvePath(path string) (*URL, error) {
	// The basic URL parser drops tabs and newlines before reading the
	// slashes ("/\t/x" is the authority form "//x").
	stripped := removeTabNewline(path)
	slash := func(i int) bool {
		return i < len(stripped) && (stripped[i] == '/' || (u.IsSpecial() && stripped[i] == '\\'))
	}
	if u.opaque || !slash(0) {
		return nil, ErrInvalidURL
	}
	if slash(1) {
		// Relative slash state: an authority follows and only the scheme
		// comes from the base.
		return Parse(u.Scheme + ":" + path)
	}
	if u.Scheme == "file" && len(u.path) > 0 && isNormalizedWindowsDriveLetter(u.path[0]) &&
		!startsWithWindowsDriveLetter(stripped[1:]) {
		// File slash state: the base's drive letter stays ("/x" against
		// file:///C:/y is file:///C:/x).
		return Parse(u.authorityPrefix() + "/" + u.path[0] + path)
	}
	return Parse(u.authorityPrefix() + path)
}

// startsWithWindowsDriveLetter is the URL Standard's "starts with a Windows
// drive letter".
func startsWithWindowsDriveLetter(s string) bool {
	if len(s) < 2 || !isWindowsDriveLetter(s[:2]) {
		return false
	}
	return len(s) == 2 || strings.ContainsRune(`/\?#`, rune(s[2]))
}

// authorityPrefix is the URL's serialization up to (not including) its path:
// "scheme:" plus "//[userinfo@]host[:port]" when it has a host.
func (u *URL) authorityPrefix() string {
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteByte(':')
	if u.HasHost {
		b.WriteString("//")
		if u.Username != "" || u.Password != "" {
			b.WriteString(u.Username)
			if u.Password != "" {
				b.WriteByte(':')
				b.WriteString(u.Password)
			}
			b.WriteByte('@')
		}
		b.WriteString(u.Host)
		if u.Port >= 0 {
			fmt.Fprintf(&b, ":%d", u.Port)
		}
	}
	return b.String()
}

func removeTabNewline(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
}
