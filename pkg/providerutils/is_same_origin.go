package providerutils

import (
	"net/url"
	"strings"
)

// IsSameOrigin reports whether urlText and baseURL have the same scheme, host,
// and port using URL origin semantics. Invalid absolute URLs fail closed.
func IsSameOrigin(urlText, baseURL string) bool {
	u, err := url.Parse(urlText)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return false
	}
	uScheme, uHost, uPort, ok := originParts(u)
	if !ok {
		return false
	}
	baseScheme, baseHost, basePort, ok := originParts(base)
	if !ok {
		return false
	}
	return uScheme == baseScheme && uHost == baseHost && uPort == basePort
}

func originParts(u *url.URL) (scheme, host, port string, ok bool) {
	scheme = strings.ToLower(u.Scheme)
	host = strings.ToLower(u.Hostname())
	if scheme == "" || host == "" {
		return "", "", "", false
	}
	port = u.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return scheme, host, port, true
}
