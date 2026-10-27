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
	// The WHATWG parser percent-decodes the host; net/url rejects it.
	normalized, err := fileutil.NormalizeEscapedURLHost(whatwgPreprocess(raw))
	if err != nil {
		return nil, false
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme == "" {
		return nil, false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Fragment = ""
	// WHATWG drops empty credentials ("https://@host/" has none).
	if parsed.User != nil {
		if password, _ := parsed.User.Password(); parsed.User.Username() == "" && password == "" {
			parsed.User = nil
		}
	}
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

// whatwgPreprocess applies the parts of the WHATWG URL parser that net/url
// does not: ASCII tabs and newlines are removed everywhere, and for http(s)
// URLs a backslash before the query or fragment is a slash.
func whatwgPreprocess(raw string) string {
	raw = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(raw)
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http:") && !strings.HasPrefix(lower, "https:") {
		return raw
	}
	end := len(raw)
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		end = i
	}
	return strings.ReplaceAll(raw[:end], "\\", "/") + raw[end:]
}
