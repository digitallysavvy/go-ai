package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
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

// TS authorizeOnce clears authPromise in .finally, so a flow that throws does
// not block later 401s. A provider callback (or the auth HTTP client) that
// panicked left Go's flight in place: every later authorization waited on it
// until its own ctx ended.
func TestTransportAuthPanickingFlowIsReleased(t *testing.T) {
	provider := newFakeOAuthProvider()
	var mu sync.Mutex
	calls := 0
	client := &http.Client{Transport: oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			panic("auth transport exploded")
		}
		return nil, errors.New("offline")
	})}
	a := newTransportAuth(provider, "https://auth.example.com/mcp", client)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected the panic to reach the caller that ran the flow")
			}
		}()
		_, _ = a.authorizeOnce(context.Background(), nil, "")
	}()
	a.mu.Lock()
	inflight := a.inflight
	a.mu.Unlock()
	if inflight != nil {
		t.Fatal("the panicked flow is still in flight")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.authorizeOnce(ctx, nil, ""); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second authorization err = %v, want a fresh flow's error", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls < 2 {
		t.Fatalf("auth requests = %d, want a second flow to run", calls)
	}
}
