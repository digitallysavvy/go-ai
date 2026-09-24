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
