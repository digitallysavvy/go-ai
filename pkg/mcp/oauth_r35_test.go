package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestOAuthClientChecksAreMCPClientOAuthError(t *testing.T) {
	err := AssertOAuthResourceMetadataURLSameOrigin("https://a.example/mcp", "https://b.example/prm")
	var oauthErr *MCPClientOAuthError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("origin mismatch must be *MCPClientOAuthError, got %T", err)
	}
	var clientErr *MCPClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("v0.5.2 callers match *MCPClientError, got %T", err)
	}
}

func TestOAuthIssuerMismatchIsMCPClientOAuthError(t *testing.T) {
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"issuer":"https://evil.example.com","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"]}`), nil
	})
	_, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	var oauthErr *MCPClientOAuthError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("issuer mismatch must be *MCPClientOAuthError, got %T %v", err, err)
	}
}

func TestRegisterOAuthClientInvalidRedirectURIFailsInference(t *testing.T) {
	called := false
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		called = true
		return jsonResponse(http.StatusOK, `{}`), nil
	})
	_, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", nil, OAuthClientMetadata{RedirectURIs: []string{"/relative/callback"}}, client)
	if err == nil || called {
		t.Fatalf("want an Invalid URL error before any request, err=%v called=%v", err, called)
	}
}
