package mcp

import (
	"context"
	"net/http"
	"net/url"
	"sync"
)

// UnauthorizedError is returned (and reported through the transport's error
// callback) when the server answered 401 and the OAuth flow could not produce
// a usable token without user interaction, matching TS UnauthorizedError
// (oauth.ts). Complete the authorization (the provider's
// RedirectToAuthorization has been called) and call Auth with the
// authorization code, then retry.
type UnauthorizedError struct {
	Message string
}

func (e *UnauthorizedError) Error() string {
	if e.Message == "" {
		return "Unauthorized"
	}
	return e.Message
}

// transportAuth drives an OAuthClientProvider for the HTTP and SSE
// transports, matching the authProvider handling in TS HttpMCPTransport and
// SseMCPTransport: bearer tokens from provider.Tokens on every request, and a
// single shared authorization attempt (Auth) for concurrent 401 responses.
type transportAuth struct {
	provider   OAuthClientProvider
	serverURL  string
	httpClient *http.Client

	mu       sync.Mutex
	inflight *authFlight
}

type authFlight struct {
	done   chan struct{}
	result AuthResult
	err    error
}

func newTransportAuth(provider OAuthClientProvider, serverURL string, httpClient *http.Client) *transportAuth {
	if provider == nil {
		return nil
	}
	return &transportAuth{provider: provider, serverURL: serverURL, httpClient: httpClient}
}

func (a *transportAuth) enabled() bool { return a != nil }

// setBearer sets the Authorization header from the provider's current tokens.
func (a *transportAuth) setBearer(ctx context.Context, header http.Header) error {
	if a == nil {
		return nil
	}
	tokens, err := a.provider.Tokens(ctx)
	if err != nil {
		return err
	}
	if tokens != nil && tokens.AccessToken != "" {
		header.Set("Authorization", "Bearer "+tokens.AccessToken)
	}
	return nil
}

// tokenChanged reports whether the provider now holds an access token other
// than the one a request was sent with (another request already refreshed it).
func (a *transportAuth) tokenChanged(ctx context.Context, requestAuthorization string) bool {
	tokens, err := a.provider.Tokens(ctx)
	if err != nil || tokens == nil || tokens.AccessToken == "" {
		return false
	}
	return requestAuthorization != "Bearer "+tokens.AccessToken
}

// authorize runs the OAuth recovery for a 401 response (TS authorizeOnce plus
// the AUTHORIZED check). It returns nil when the request can be retried.
func (a *transportAuth) authorize(ctx context.Context, resp *http.Response) error {
	params := ExtractWWWAuthenticateParams(resp)
	result, err := a.authorizeOnce(ctx, params.ResourceMetadataURL, params.Scope)
	if err != nil {
		return err
	}
	if result != AuthResultAuthorized {
		return &UnauthorizedError{}
	}
	return nil
}

func (a *transportAuth) authorizeOnce(ctx context.Context, resourceMetadataURL *url.URL, scope string) (AuthResult, error) {
	a.mu.Lock()
	flight := a.inflight
	if flight == nil {
		flight = &authFlight{done: make(chan struct{})}
		a.inflight = flight
		a.mu.Unlock()
		flight.result, flight.err = Auth(ctx, a.provider, AuthOptions{
			ServerURL:           a.serverURL,
			ResourceMetadataURL: resourceMetadataURL,
			Scope:               scope,
			HTTPClient:          a.httpClient,
		})
		a.mu.Lock()
		a.inflight = nil
		a.mu.Unlock()
		close(flight.done)
		return flight.result, flight.err
	}
	a.mu.Unlock()

	select {
	case <-flight.done:
		return flight.result, flight.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
