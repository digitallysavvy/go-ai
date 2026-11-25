package providerutils

import "net/http"

// IsCrossOriginRedirectHop reports whether req is a redirect hop (set by
// net/http's Client while following a 3xx) whose origin, or the origin of
// any earlier hop of its redirect chain, differs from the first request of
// the chain. Credential transports run once per hop, so they must not
// re-add credentials on such a hop: TS adds its headers once per call, and
// fetch drops Authorization on a cross-origin redirect and never adds it
// back, so a chain A -> B -> A reaches A again without it. Go is stricter
// than TS for custom credential headers such as x-goog-api-key (S2:
// security is never weaker than TS). An unparsable URL counts as
// cross-origin (fail closed).
func IsCrossOriginRedirectHop(req *http.Request) bool {
	if req == nil || req.Response == nil {
		return false
	}
	var chain []*http.Request
	first := req
	for first.Response != nil && first.Response.Request != nil {
		chain = append(chain, first)
		first = first.Response.Request
	}
	if first == req || first.URL == nil {
		return true
	}
	for _, hop := range chain {
		if hop.URL == nil || !IsSameOrigin(hop.URL.String(), first.URL.String()) {
			return true
		}
	}
	return false
}
