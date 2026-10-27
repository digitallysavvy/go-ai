package toolwebfetch

import (
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	providerrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// normalizeHostname lowercases, maps IDN to punycode and strips trailing dots
// so `example.com.` cannot bypass a filter for `example.com`.
func normalizeHostname(hostname string) string {
	return fileutil.NormalizeHostname(hostname)
}

// normalizeDomains validates bare domains such as "example.com" (no scheme,
// port, path, credentials or wildcards) and normalizes them.
func normalizeDomains(parameter string, domains []string) ([]string, error) {
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		trimmed := strings.TrimSpace(domain)
		if trimmed == "" || strings.ContainsAny(trimmed, " \t\r\n\f\v/\\:*@?#[]") {
			return nil, &providerrors.InvalidArgumentError{
				Field:   parameter,
				Message: `Invalid domain "` + domain + `". Use bare domains such as "example.com"; subdomains are included automatically.`,
			}
		}
		parsed, err := url.Parse("https://" + trimmed)
		if err != nil || parsed.Hostname() == "" {
			return nil, &providerrors.InvalidArgumentError{Field: parameter, Message: `Invalid domain "` + domain + `".`}
		}
		out = append(out, normalizeHostname(parsed.Hostname()))
	}
	return out, nil
}

// domainFilter reports whether a URL's host is permitted. allowed wins over
// blocked; with neither set every host passes.
type domainFilter struct {
	allowed                []string
	blocked                []string
	hasAllowed, hasBlocked bool
}

func (f domainFilter) allows(u *url.URL) bool {
	hostname := normalizeHostname(u.Hostname())
	matches := func(domains []string) bool {
		for _, domain := range domains {
			if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
				return true
			}
		}
		return false
	}
	if f.hasAllowed {
		return matches(f.allowed)
	}
	if f.hasBlocked {
		return !matches(f.blocked)
	}
	return true
}
