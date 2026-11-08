package providerutils

import "net/http"

// IsCrossOriginRedirectHop reports whether req is a redirect hop (set by
// net/http's Client while following a 3xx) whose origin differs from the
// first request of the redirect chain. Credential transports run once per
// hop, so they must not re-add credentials on such a hop: TS adds its
// headers once per call, and fetch drops Authorization on a cross-origin
// redirect. Go is stricter than TS for custom credential headers such as
// x-goog-api-key (S2: security is never weaker than TS). An unparsable URL
// counts as cross-origin (fail closed).
func IsCrossOriginRedirectHop(req *http.Request) bool {
	if req == nil || req.Response == nil {
		return false
	}
	first := req
	for first.Response != nil && first.Response.Request != nil {
		first = first.Response.Request
	}
	if first == req || first.URL == nil || req.URL == nil {
		return true
	}
	return !IsSameOrigin(req.URL.String(), first.URL.String())
}
