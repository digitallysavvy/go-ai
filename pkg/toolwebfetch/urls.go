package toolwebfetch

import (
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// canonicalURL parses an absolute http(s) URL and returns it in a canonical
// form without the fragment (WHATWG `new URL(url)` with `hash = ”`): scheme
// and host lowercased, IDN hosts in punycode, default port removed, and an
// empty path replaced by "/". ok is false when the URL cannot be parsed.
func canonicalURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return nil, false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		if parsed.Host == "" {
			return nil, false
		}
		host := strings.ToLower(parsed.Hostname())
		if ascii := fileutilASCIIHost(host); ascii != "" {
			host = ascii
		}
		port := parsed.Port()
		if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
			port = ""
		}
		if strings.Contains(host, ":") { // IPv6 literal
			host = "[" + host + "]"
		}
		if port != "" {
			host += ":" + port
		}
		parsed.Host = host
		if parsed.Path == "" && parsed.Opaque == "" {
			parsed.Path = "/"
		}
	}
	return parsed, true
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
