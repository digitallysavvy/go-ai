// Package internal holds small helpers shared between the Google Vertex
// provider and its independently-buildable sub-providers (anthropic, xai).
// Those sub-providers intentionally do not import the parent googlevertex
// package -- doing so would create an import cycle, since the parent
// constructs and imports them -- so shared, provider-agnostic plumbing like
// the OAuth Bearer transport lives here instead of being duplicated in each
// package. Go's "internal" visibility rule already restricts importers to
// packages rooted at pkg/providers/googlevertex, which is exactly the set
// that needs this.
package internal

import (
	"context"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

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
	return !providerutils.IsSameOrigin(req.URL.String(), first.URL.String())
}

// AuthTransport injects a bearer token resolved by TokenFunc into every
// outgoing request's Authorization header before delegating to Base. A
// redirect hop to another origin gets no token (IsCrossOriginRedirectHop).
type AuthTransport struct {
	Base      http.RoundTripper
	TokenFunc func(ctx context.Context) (string, error)
	// OverrideAuthorization makes the resolved token replace any
	// caller-supplied Authorization header (the TS node xai wrapper spreads
	// the generated bearer token after the user's headers).
	OverrideAuthorization bool
}

// RoundTrip implements http.RoundTripper.
func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.roundTrip(req)
	// A RoundTripper closes the request body, even on errors (a
	// credential or signing failure returns before the base transport).
	if err != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	return resp, err
}

func (t *AuthTransport) roundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if IsCrossOriginRedirectHop(req) {
		return t.Base.RoundTrip(clone)
	}
	// A caller-supplied Authorization header wins, like TS where the user's
	// headers are spread after the generated bearer token.
	if !t.OverrideAuthorization && clone.Header.Get("Authorization") != "" {
		return t.Base.RoundTrip(clone)
	}
	token, err := t.TokenFunc(req.Context())
	if err != nil {
		return nil, internalhttp.RejectFetch(err)
	}
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.Base.RoundTrip(clone)
}
