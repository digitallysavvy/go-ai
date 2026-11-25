package urlnorm

import (
	"strconv"
	"strings"
)

// Origin returns the URL's ASCII serialized origin (`URL.prototype.origin`):
// "scheme://host[:port]" for the special schemes other than file (the port
// is already null when it is the scheme's default), the origin of the inner
// URL for "blob:" URLs whose inner URL is http(s), and "null" (an opaque
// origin) for everything else.
func (u *URL) Origin() string {
	switch u.Scheme {
	case "ftp", "http", "https", "ws", "wss":
		var b strings.Builder
		b.WriteString(u.Scheme)
		b.WriteString("://")
		b.WriteString(u.Host)
		if u.Port >= 0 {
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(u.Port))
		}
		return b.String()
	case "blob":
		inner, err := Parse(u.Pathname())
		if err == nil && (inner.Scheme == "http" || inner.Scheme == "https") {
			return inner.Origin()
		}
	}
	return "null"
}

// RequestOrigin returns the origin of raw when it parses as an http: or https:
// URL, and "" when it does not parse or has another scheme. It is TS
// `getRequestOrigin` (ai/src/telemetry/get-request-origin.ts, 934100cad4):
// the origin drops credentials, the path, the query and the fragment.
func RequestOrigin(raw string) string {
	u, err := Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Origin()
}
