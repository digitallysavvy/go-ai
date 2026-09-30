package xai

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// defaultCloudPlatformAuthScope is the OAuth2 scope requested when falling
// back to Application Default Credentials, matching
// pkg/providers/googlevertex/anthropic's defaultCloudPlatformAuth.
const defaultCloudPlatformAuthScope = "https://www.googleapis.com/auth/cloud-platform"

// authTransport injects a fresh OAuth2 Bearer token into every outgoing
// request. Duplicated (rather than imported) from the unexported
// authTransport types in pkg/providers/googlevertex/provider.go and
// pkg/providers/googlevertex/anthropic/auth.go: this package intentionally
// does not depend on the parent googlevertex package (see provider.go's
// package doc) so it cannot reuse those.
type authTransport struct {
	base      http.RoundTripper
	tokenFunc func(ctx context.Context) (string, error)
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	token, err := t.tokenFunc(req.Context())
	if err != nil {
		return nil, err
	}
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(clone)
}

// resolveAuthToken builds a token-resolver function from Config, mirroring
// TS's node/edge createGoogleVertexXai wrappers (google-vertex-xai-provider-node.ts
// / edge/google-vertex-xai-provider-edge.ts): an explicit AccessToken or
// AuthToken callback take precedence; a TokenSource is used next; absent all
// three, Application Default Credentials are loaded lazily and cached after
// the first successful resolution, matching google-auth-library's default
// behavior for the un-wrapped TS provider.
func (cfg Config) resolveAuthToken() func(ctx context.Context) (string, error) {
	if cfg.AuthToken != nil {
		return cfg.AuthToken
	}
	if cfg.AccessToken != "" {
		token := cfg.AccessToken
		return func(context.Context) (string, error) { return token, nil }
	}
	if cfg.TokenSource != nil {
		ts := cfg.TokenSource
		return func(context.Context) (string, error) {
			token, err := ts.Token()
			if err != nil {
				return "", fmt.Errorf("failed to resolve vertex auth token: %w", err)
			}
			if token == nil || token.AccessToken == "" {
				return "", fmt.Errorf("resolved empty vertex auth token")
			}
			return token.AccessToken, nil
		}
	}

	var mu sync.Mutex
	var source oauth2.TokenSource
	return func(ctx context.Context) (string, error) {
		mu.Lock()
		if source == nil {
			creds, err := google.FindDefaultCredentials(ctx, defaultCloudPlatformAuthScope)
			if err != nil {
				mu.Unlock()
				return "", fmt.Errorf("failed to load Google application default credentials: %w", err)
			}
			source = creds.TokenSource
		}
		current := source
		mu.Unlock()

		token, err := current.Token()
		if err != nil {
			return "", fmt.Errorf("failed to fetch Google access token: %w", err)
		}
		if token == nil || token.AccessToken == "" {
			return "", fmt.Errorf("google token source returned an empty access token")
		}
		return token.AccessToken, nil
	}
}
