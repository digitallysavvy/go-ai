package toolwebfetch

import (
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	providerrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
		trimmed := providerutils.JSTrim(domain) // TS domain.trim()
		// TS /[\s/\\:*@?#[\]]/: JS \s is the JS trim whitespace set.
		if trimmed == "" || strings.ContainsFunc(trimmed, func(r rune) bool {
			return providerutils.IsJSWhiteSpace(r) || strings.ContainsRune("/\\:*@?#[]", r)
		}) {
			return nil, &providerrors.InvalidArgumentError{
				Field:   parameter,
				Message: `Invalid domain "` + domain + `". Use bare domains such as "example.com"; subdomains are included automatically.`,
			}
		}
		// TS new URL(`https://${trimmed}`): the WHATWG parser, which rejects
		// hosts net/url accepts (e.g. a C1 control such as U+0085).
		parsed, ok := canonicalURL("https://" + trimmed)
		if !ok || parsed.Hostname() == "" {
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
