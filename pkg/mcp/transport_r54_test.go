package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
)

// tokensFailAfterProvider returns its tokens for the first okCalls Tokens
// calls and errTokens after that.
type tokensFailAfterProvider struct {
	*fakeOAuthProvider
	mu      sync.Mutex
	okCalls int
	calls   int
}

var errTokensStore = errors.New("token store unavailable")

func (p *tokensFailAfterProvider) Tokens(ctx context.Context) (*OAuthTokens, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.mu.Unlock()
	if n > p.okCalls {
		return nil, errTokensStore
	}
	return p.fakeOAuthProvider.Tokens(ctx)
}

// TS SseMCPTransport.send: on a 401, accessTokenChanged awaits
// authProvider.tokens(); when that throws, the error leaves attempt() (reported
// once through onerror) and auth() never runs. Go swallowed the error and ran
// the OAuth flow.
func TestSSETransportSend401TokensErrorInAccessTokenChangedFailsWithoutAuth(t *testing.T) {
	base, _ := refreshingAuthFixture(t)
	provider := &tokensFailAfterProvider{fakeOAuthProvider: base, okCalls: 1}
	var authRequests int
	var authMu sync.Mutex
	authClient := &http.Client{Transport: oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		authMu.Lock()
		authRequests++
		authMu.Unlock()
		return nil, errors.New("auth flow must not run")
	})}
	server := &authServerSSEClient{want: "Bearer NEW_AT"}
	var reported []error
	transport := NewSSETransport(SSETransportConfig{
		URL: "https://auth.example.com/mcp", SSEClient: server, AuthProvider: provider, AuthHTTPClient: authClient,
		OnError: func(err error) { reported = append(reported, err) },
	})
	transport.connected = true
	transport.endpoint = transport.baseURL.ResolveReference(&url.URL{Path: "/messages"})
	msg, _ := CreateRequest(1, "ping", nil)
	err := transport.Send(context.Background(), msg)
	if !errors.Is(err, errTokensStore) {
		t.Fatalf("err = %v, want the Tokens error", err)
	}
	if len(reported) != 1 || !errors.Is(reported[0], errTokensStore) {
		t.Fatalf("reported = %v, want the Tokens error once", reported)
	}
	authMu.Lock()
	defer authMu.Unlock()
	if authRequests != 0 {
		t.Fatalf("auth flow made %d requests, want 0", authRequests)
	}
	if len(server.requests) != 1 {
		t.Fatalf("server requests = %v, want 1 (no retry)", server.requests)
	}
}
