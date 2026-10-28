package mcp

import (
	"context"
	"errors"
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
	// canceled is set when the flow failed because the caller that ran it
	// canceled its ctx.
	canceled bool
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
// A Tokens error is returned, as TS accessTokenChanged lets tokens() throw out
// of the request instead of running the OAuth flow.
func (a *transportAuth) tokenChanged(ctx context.Context, requestAuthorization string) (bool, error) {
	tokens, err := a.provider.Tokens(ctx)
	if err != nil {
		return false, err
	}
	if tokens == nil || tokens.AccessToken == "" {
		return false, nil
	}
	return requestAuthorization != "Bearer "+tokens.AccessToken, nil
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
	for {
		a.mu.Lock()
		flight := a.inflight
		if flight == nil {
			flight = &authFlight{done: make(chan struct{})}
			a.inflight = flight
			a.mu.Unlock()
			a.runFlight(ctx, flight, resourceMetadataURL, scope)
			return flight.result, flight.err
		}
		a.mu.Unlock()

		select {
		case <-flight.done:
			// TS authorizeOnce shares one auth() promise that no request's
			// abort signal cancels. Go runs the flow under the first caller's
			// ctx, so a flow that ended because that caller gave up is not
			// this caller's result: run it again.
			if flight.canceled && ctx.Err() == nil {
				continue
			}
			return flight.result, flight.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// runFlight runs the shared OAuth flow and always releases it, like TS
// authorizeOnce's `.finally(() => { this.authPromise = undefined })`: a
// provider callback that panics must not leave the flight in place, or every
// later 401 would wait on it until its own ctx ended. The waiters then see an
// error and the panic continues in the caller that ran the flow.
func (a *transportAuth) runFlight(ctx context.Context, flight *authFlight, resourceMetadataURL *url.URL, scope string) {
	completed := false
	defer func() {
		if !completed {
			flight.err = errAuthFlowPanicked
		}
		flight.canceled = flight.err != nil && ctx.Err() != nil
		a.mu.Lock()
		a.inflight = nil
		a.mu.Unlock()
		close(flight.done)
	}()
	flight.result, flight.err = Auth(ctx, a.provider, AuthOptions{
		ServerURL:           a.serverURL,
		ResourceMetadataURL: resourceMetadataURL,
		Scope:               scope,
		HTTPClient:          a.httpClient,
	})
	completed = true
}

// errAuthFlowPanicked is what requests waiting on a shared OAuth flow get when
// the flow panicked in the request that ran it.
var errAuthFlowPanicked = errors.New("mcp: OAuth authorization panicked")
