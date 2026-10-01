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
)

// AuthTransport injects a bearer token resolved by TokenFunc into every
// outgoing request's Authorization header before delegating to Base.
type AuthTransport struct {
	Base      http.RoundTripper
	TokenFunc func(ctx context.Context) (string, error)
}

// RoundTrip implements http.RoundTripper.
func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	token, err := t.TokenFunc(req.Context())
	if err != nil {
		return nil, err
	}
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.Base.RoundTrip(clone)
}
