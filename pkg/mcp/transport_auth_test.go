package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// authServerSSEClient answers 401 (with a Bearer challenge) until it sees the
// expected bearer token, then 200 JSON.
type authServerSSEClient struct {
	mu       sync.Mutex
	want     string
	requests []string
}

func (c *authServerSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req.Header.Get("Authorization"))
	c.mu.Unlock()
	if req.Header.Get("Authorization") != c.want {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Www-Authenticate": []string{`Bearer scope="mcp"`}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`)),
	}, nil
}

func refreshingAuthFixture(t *testing.T) (*fakeOAuthProvider, *http.Client) {
	t.Helper()
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
	provider.tokens = &OAuthTokens{
		AccessToken:         "OLD_AT",
		RefreshToken:        "RT",
		Issuer:              "https://auth.example.com",
		AuthorizationServer: "https://auth.example.com/",
		TokenEndpoint:       "https://auth.example.com/token",
	}
	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"access_token":"NEW_AT","token_type":"Bearer"}`), nil
		},
	})
	return provider, client
}

// TS HttpMCPTransport: a 401 runs auth() once and the request is retried with
// the new token.
func TestHTTPTransportAuthProviderRecoversFrom401(t *testing.T) {
	provider, oauthClient := refreshingAuthFixture(t)
	server := &authServerSSEClient{want: "Bearer NEW_AT"}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL: "https://auth.example.com/mcp", SSEClient: server, AuthProvider: provider, AuthHTTPClient: oauthClient,
	})
	transport.connected = true
	msg, _ := CreateRequest(1, "ping", nil)
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := server.requests; len(got) != 2 || got[0] != "Bearer OLD_AT" || got[1] != "Bearer NEW_AT" {
		t.Fatalf("authorization headers = %v", got)
	}
	if _, err := transport.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// When auth() needs the user (REDIRECT), the request fails with
// UnauthorizedError and is reported through OnError.
func TestHTTPTransportAuthProviderReportsUnauthorized(t *testing.T) {
	provider := newFakeOAuthProvider()
	oauthClient := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/register": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusCreated, `{"client_id":"c","redirect_uris":["https://app.example.com/callback"]}`), nil
		},
	})
	var reported []error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL: "https://auth.example.com/mcp", SSEClient: &authServerSSEClient{want: "Bearer never"},
		AuthProvider: provider, AuthHTTPClient: oauthClient,
		OnError: func(err error) { reported = append(reported, err) },
	})
	transport.connected = true
	msg, _ := CreateRequest(1, "ping", nil)
	err := transport.Send(context.Background(), msg)
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("err = %v, want UnauthorizedError", err)
	}
	if len(reported) != 1 || !errors.As(reported[0], &unauthorized) {
		t.Fatalf("reported = %v", reported)
	}
	if len(provider.redirectedTo) != 1 {
		t.Fatalf("expected the provider to be asked to redirect, got %v", provider.redirectedTo)
	}
}

func TestSSETransportAuthProviderRecoversFrom401OnPost(t *testing.T) {
	provider, oauthClient := refreshingAuthFixture(t)
	server := &authServerSSEClient{want: "Bearer NEW_AT"}
	transport := NewSSETransport(SSETransportConfig{
		URL: "https://auth.example.com/mcp", SSEClient: server, AuthProvider: provider, AuthHTTPClient: oauthClient,
	})
	transport.connected = true
	transport.endpoint = transport.baseURL.ResolveReference(&url.URL{Path: "/messages"})
	msg, _ := CreateRequest(1, "ping", nil)
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := server.requests; len(got) != 2 || got[1] != "Bearer NEW_AT" {
		t.Fatalf("authorization headers = %v", got)
	}
}
