package fileutil

import (
	"net/http"
	neturl "net/url"
)

// IsSameOrigin reports whether a and b are absolute URLs with the same
// scheme, host and port (TS isSameOrigin). Invalid URLs fail closed.
func IsSameOrigin(a, b string) bool {
	ua, err := neturl.Parse(a)
	if err != nil || ua.Scheme == "" || ua.Host == "" {
		return false
	}
	ub, err := neturl.Parse(b)
	if err != nil || ub.Scheme == "" || ub.Host == "" {
		return false
	}
	return sameOrigin(ua, ub)
}

// TrustedOriginDownloadOptions returns download options for a URL taken from
// a provider response, mirroring TS getFromApi({validateUrl: true,
// trustedOrigin}). Hops that are same-origin with trustedOrigin (the
// developer-configured base URL, never response data) skip target validation
// and use trustedTransport (nil means http.DefaultTransport); every other hop
// is validated with ValidateDownloadURL and dialed through SafeTransport.
func TrustedOriginDownloadOptions(trustedOrigin string, trustedTransport http.RoundTripper) DownloadOptions {
	opts := DefaultDownloadOptions()
	if trustedTransport == nil {
		trustedTransport = http.DefaultTransport
	}
	opts.URLValidator = func(raw string) error {
		if IsSameOrigin(raw, trustedOrigin) {
			return nil
		}
		return ValidateDownloadURL(raw)
	}
	opts.Transport = originRoutingTransport{trustedOrigin: trustedOrigin, trusted: trustedTransport, safe: opts.Transport}
	return opts
}

type originRoutingTransport struct {
	trustedOrigin string
	trusted       http.RoundTripper
	safe          http.RoundTripper
}

func (t originRoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if IsSameOrigin(req.URL.String(), t.trustedOrigin) {
		return t.trusted.RoundTrip(req)
	}
	return t.safe.RoundTrip(req)
}

// TrustRoutingTransport is the generalization of the per-hop routing behind
// TrustedOriginDownloadOptions for providers whose trust boundary is broader
// than a single fixed origin (e.g. a provider that trusts both its
// developer-configured base URL and a wildcard SaaS domain such as
// "*.example.com"). Every request is routed to trusted when isTrusted(url)
// reports true, otherwise to safe (normally SafeTransport(), which pins DNS
// results at connect time). A nil trusted defaults to http.DefaultTransport,
// matching plain-fetch semantics for a developer-trusted origin (TS
// fetchWithValidatedRedirects, which chooses the fetch per hop).
func TrustRoutingTransport(isTrusted func(rawURL string) bool, trusted, safe http.RoundTripper) http.RoundTripper {
	if trusted == nil {
		trusted = http.DefaultTransport
	}
	return trustRoutingTransport{isTrusted: isTrusted, trusted: trusted, safe: safe}
}

// TrustedURLValidator returns a DownloadOptions.URLValidator that skips
// ValidateDownloadURL for hops isTrusted reports as trusted (a
// developer-configured, possibly self-hosted, origin should not be rejected
// by the generic SSRF blocklist) and otherwise applies it, mirroring the
// per-hop validation behind TrustedOriginDownloadOptions.
func TrustedURLValidator(isTrusted func(rawURL string) bool) func(string) error {
	return func(raw string) error {
		if isTrusted(raw) {
			return nil
		}
		return ValidateDownloadURL(raw)
	}
}

type trustRoutingTransport struct {
	isTrusted func(rawURL string) bool
	trusted   http.RoundTripper
	safe      http.RoundTripper
}

func (t trustRoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.isTrusted(req.URL.String()) {
		return t.trusted.RoundTrip(req)
	}
	return t.safe.RoundTrip(req)
}
