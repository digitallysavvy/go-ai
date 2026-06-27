package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestMCPOAuthStateParamValidation verifies that a mismatched state param
// causes ValidateOAuthState to return an error (CSRF protection).
func TestMCPOAuthStateParamValidation(t *testing.T) {
	err := ValidateOAuthState("returned-state-xyz", "sent-state-abc")
	if err == nil {
		t.Fatal("expected error for mismatched state params, got nil")
	}
	if !strings.Contains(err.Error(), "CSRF") {
		t.Errorf("error message should mention CSRF, got: %q", err.Error())
	}
}

// TestMCPOAuthStateParamMatchSucceeds verifies that a matching state param
// causes ValidateOAuthState to return nil (authorization proceeds normally).
func TestMCPOAuthStateParamMatchSucceeds(t *testing.T) {
	state := "random-state-1234567890"
	if err := ValidateOAuthState(state, state); err != nil {
		t.Errorf("expected nil for matching state params, got: %v", err)
	}
}

// TestMCPOAuthStateEmptyVsNonEmpty verifies that an empty returned state does
// not match a non-empty sent state.
func TestMCPOAuthStateEmptyVsNonEmpty(t *testing.T) {
	err := ValidateOAuthState("", "some-state")
	if err == nil {
		t.Fatal("expected error when returned state is empty, got nil")
	}
}

type oauthRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f oauthRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func oauthTestClient(fn oauthRoundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestExtractResourceMetadataURLMatchesTypeScript(t *testing.T) {
	resp := &http.Response{Header: make(http.Header)}
	resp.Header.Set("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="https://resource.example.com/.well-known/oauth-protected-resource"`)

	got, ok := ExtractResourceMetadataURL(resp)
	if !ok {
		t.Fatal("expected resource metadata URL")
	}
	if got.String() != "https://resource.example.com/.well-known/oauth-protected-resource" {
		t.Fatalf("url = %s", got.String())
	}

	resp.Header.Set("WWW-Authenticate", `Basic realm="mcp", resource_metadata="https://resource.example.com/metadata"`)
	if _, ok := ExtractResourceMetadataURL(resp); ok {
		t.Fatal("expected non-Bearer header to be ignored")
	}
}

func TestOAuthProtectedResourceMetadataDiscoveryPathFallbackAndHeader(t *testing.T) {
	var calls []string
	var protocolHeaders []string
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.String())
		protocolHeaders = append(protocolHeaders, req.Header.Get("MCP-Protocol-Version"))
		switch len(calls) {
		case 1:
			return jsonResponse(http.StatusNotFound, `not found`), nil
		case 2:
			return jsonResponse(http.StatusOK, `{"resource":"https://resource.example.com","authorization_servers":["https://auth.example.com"]}`), nil
		default:
			t.Fatalf("unexpected call %d", len(calls))
			return nil, errors.New("unexpected call")
		}
	})

	metadata, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com/mcp/server?x=1", OAuthDiscoveryOptions{
		ProtocolVersion: "2025-06-18",
		HTTPClient:      client,
	})
	if err != nil {
		t.Fatalf("DiscoverOAuthProtectedResourceMetadata error = %v", err)
	}
	if metadata.Resource != "https://resource.example.com" || len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != "https://auth.example.com" {
		t.Fatalf("metadata = %#v", metadata)
	}
	wantCalls := []string{
		"https://resource.example.com/.well-known/oauth-protected-resource/mcp/server?x=1",
		"https://resource.example.com/.well-known/oauth-protected-resource",
	}
	if strings.Join(calls, "\n") != strings.Join(wantCalls, "\n") {
		t.Fatalf("calls = %#v", calls)
	}
	if protocolHeaders[0] != "2025-06-18" || protocolHeaders[1] != "2025-06-18" {
		t.Fatalf("protocol headers = %#v", protocolHeaders)
	}
}

func TestOAuthProtectedResourceMetadataExplicitURLDoesNotFallback(t *testing.T) {
	var calls int
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != "https://custom.example.com/metadata" {
			t.Fatalf("request URL = %s", req.URL.String())
		}
		return jsonResponse(http.StatusNotFound, `not found`), nil
	})

	_, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com/mcp", OAuthDiscoveryOptions{
		ResourceMetadataURL: "https://custom.example.com/metadata",
		HTTPClient:          client,
	})
	if err == nil || !strings.Contains(err.Error(), "Resource server does not implement OAuth 2.0 Protected Resource Metadata.") {
		t.Fatalf("error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestBuildAuthorizationServerDiscoveryURLsMatchesTypeScript(t *testing.T) {
	got, err := BuildAuthorizationServerDiscoveryURLs("https://auth.example.com/tenant1/")
	if err != nil {
		t.Fatalf("BuildAuthorizationServerDiscoveryURLs error = %v", err)
	}
	want := []OAuthDiscoveryURL{
		{URL: "https://auth.example.com/.well-known/oauth-authorization-server/tenant1", Type: "oauth", ExpectedIssuer: "https://auth.example.com/tenant1"},
		{URL: "https://auth.example.com/.well-known/oauth-authorization-server", Type: "oauth", ExpectedIssuer: "https://auth.example.com"},
		{URL: "https://auth.example.com/.well-known/openid-configuration/tenant1", Type: "oidc", ExpectedIssuer: "https://auth.example.com/tenant1"},
		{URL: "https://auth.example.com/tenant1/.well-known/openid-configuration", Type: "oidc", ExpectedIssuer: "https://auth.example.com/tenant1"},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("url[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestDiscoverAuthorizationServerMetadataValidatesIssuer(t *testing.T) {
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://auth.example.com/.well-known/oauth-authorization-server/tenant1" {
			t.Fatalf("request URL = %s", req.URL.String())
		}
		return jsonResponse(http.StatusOK, `{"issuer":"https://login.real-idp.com","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"],"code_challenge_methods_supported":["S256"]}`), nil
	})

	_, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com/tenant1", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "does not match expected issuer") {
		t.Fatalf("error = %v", err)
	}
}

func TestDiscoverAuthorizationServerMetadataSkips4xxAndRequiresOIDCS256(t *testing.T) {
	var calls []string
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.String())
		switch len(calls) {
		case 1:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		case 2:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		case 3:
			return jsonResponse(http.StatusOK, `{"issuer":"https://auth.example.com/tenant1","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","jwks_uri":"https://auth.example.com/jwks.json","response_types_supported":["code"],"subject_types_supported":["public"],"id_token_signing_alg_values_supported":["RS256"],"code_challenge_methods_supported":["plain"]}`), nil
		default:
			t.Fatalf("unexpected call")
			return nil, errors.New("unexpected call")
		}
	})

	_, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com/tenant1", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "does not support S256") {
		t.Fatalf("error = %v", err)
	}
}

func TestOAuthCredentialPinningAndResourceMetadataOrigin(t *testing.T) {
	if err := AssertOAuthResourceMetadataURLSameOrigin("https://api.example.com/mcp", "https://evil.example.com/.well-known/oauth-protected-resource"); err == nil || !strings.Contains(err.Error(), "must have the same origin") {
		t.Fatalf("same-origin error = %v", err)
	}

	stored := OAuthAuthorizationServerInformation{
		AuthorizationServerURL: "https://auth.example.com/",
		TokenEndpoint:          "https://auth.example.com/token",
	}
	current := OAuthAuthorizationServerInformation{
		AuthorizationServerURL: "https://evil.example/",
		TokenEndpoint:          "https://evil.example/token",
	}
	if err := AssertOAuthAuthorizationServerInformationMatches(stored, current); err == nil || !strings.Contains(err.Error(), "does not match the metadata that issued the stored credentials") {
		t.Fatalf("pinning error = %v", err)
	}
}

func TestCreateOAuthAuthorizationServerInformationUsesMetadataTokenEndpoint(t *testing.T) {
	got, err := CreateOAuthAuthorizationServerInformation("https://auth.example.com", &OAuthAuthorizationServerMetadata{
		TokenEndpoint: "https://tokens.example.com/oauth/token",
	})
	if err != nil {
		t.Fatalf("CreateOAuthAuthorizationServerInformation error = %v", err)
	}
	if got.AuthorizationServerURL != "https://auth.example.com/" || got.TokenEndpoint != "https://tokens.example.com/oauth/token" {
		t.Fatalf("info = %#v", got)
	}
}

func TestSelectOAuthAuthorizationServerURLRunsValidationBeforeMetadataFetch(t *testing.T) {
	var calls []string
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.String())
		if strings.Contains(req.URL.String(), "oauth-authorization-server") {
			t.Fatalf("authorization server metadata should not be fetched before validation")
		}
		return jsonResponse(http.StatusOK, `{"resource":"https://api.example.com/mcp-server","authorization_servers":["https://evil.example"]}`), nil
	})
	var validatedServer string
	var validatedAS string

	_, _, err := SelectOAuthAuthorizationServerURL(context.Background(), "https://api.example.com/mcp-server", OAuthDiscoveryOptions{
		HTTPClient: client,
		ValidateAuthorizationServerURL: func(serverURL string, authorizationServerURL string) error {
			validatedServer = serverURL
			validatedAS = authorizationServerURL
			return errors.New("Unexpected authorization server")
		},
	})
	if err == nil || err.Error() != "Unexpected authorization server" {
		t.Fatalf("error = %v", err)
	}
	if validatedServer != "https://api.example.com/mcp-server" || validatedAS != "https://evil.example" {
		t.Fatalf("validated = %q %q", validatedServer, validatedAS)
	}
	if len(calls) != 1 || calls[0] != "https://api.example.com/.well-known/oauth-protected-resource/mcp-server" {
		t.Fatalf("calls = %#v", calls)
	}
}
