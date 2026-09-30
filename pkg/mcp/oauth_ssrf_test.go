package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func redirectResponse(location string) *http.Response {
	resp := jsonResponse(http.StatusFound, "")
	resp.Header.Set("Location", location)
	return resp
}

const validOAuthASMetadata = `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"],"code_challenge_methods_supported":["S256"]}`

// TS oauth.test.ts: "rejects redirects from protected resource discovery to
// private addresses" (c43e4b7).
func TestOAuthPRMDiscoveryRejectsRedirectToPrivateAddress(t *testing.T) {
	var calls int
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		return redirectResponse("http://169.254.169.254/latest/meta-data"), nil
	})
	_, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com/mcp", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "169.254.169.254") {
		t.Fatalf("err = %v, want rejection naming 169.254.169.254", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestOAuthPRMDiscoveryFollowsSameOriginRedirect(t *testing.T) {
	var calls []string
	var headers []string
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.String())
		headers = append(headers, req.Header.Get("MCP-Protocol-Version"))
		if len(calls) == 1 {
			return redirectResponse("/moved/.well-known/oauth-protected-resource"), nil
		}
		return jsonResponse(http.StatusOK, `{"resource":"https://resource.example.com","authorization_servers":["https://auth.example.com"]}`), nil
	})
	metadata, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com", OAuthDiscoveryOptions{HTTPClient: client, ProtocolVersion: "2025-06-18"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if metadata.Resource != "https://resource.example.com" || len(calls) != 2 {
		t.Fatalf("metadata=%#v calls=%v", metadata, calls)
	}
	if calls[1] != "https://resource.example.com/moved/.well-known/oauth-protected-resource" || headers[1] != "2025-06-18" {
		t.Fatalf("same-origin hop calls=%v headers=%v", calls, headers)
	}
}

func TestOAuthDiscoveryDropsProtocolHeaderOnCrossOriginRedirect(t *testing.T) {
	var headers []string
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		headers = append(headers, req.Header.Get("MCP-Protocol-Version"))
		if len(headers) == 1 {
			return redirectResponse("https://metadata.example.net/prm"), nil
		}
		return jsonResponse(http.StatusOK, `{"resource":"https://resource.example.com"}`), nil
	})
	if _, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com", OAuthDiscoveryOptions{HTTPClient: client}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(headers) != 2 || headers[0] == "" || headers[1] != "" {
		t.Fatalf("headers = %#v, want protocol header dropped on cross-origin hop", headers)
	}
}

// TS oauth.test.ts: "rejects private authorization server discovery targets".
func TestOAuthASDiscoveryRejectsPrivateTarget(t *testing.T) {
	var calls int
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(http.StatusOK, `{}`), nil
	})
	_, err := DiscoverAuthorizationServerMetadata(context.Background(), "http://169.254.169.254/latest/meta-data", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "169.254.169.254") {
		t.Fatalf("err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

// TS oauth.test.ts: "rejects authorization server discovery redirects to
// private addresses".
func TestOAuthASDiscoveryRejectsRedirectToPrivateAddress(t *testing.T) {
	var calls int
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		return redirectResponse("http://127.0.0.1:4000/.well-known/openid-configuration"), nil
	})
	_, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestOAuthASDiscoveryTrustedLoopbackOrigin(t *testing.T) {
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, strings.ReplaceAll(validOAuthASMetadata, "https://auth.example.com", "http://127.0.0.1:4000")), nil
	})
	trusted := TrustedOAuthAuthorizationServerOrigin("http://localhost:3000/mcp", "http://127.0.0.1:4000")
	if trusted != "http://127.0.0.1:4000" {
		t.Fatalf("trusted origin = %q", trusted)
	}
	if _, err := DiscoverAuthorizationServerMetadata(context.Background(), "http://127.0.0.1:4000", OAuthDiscoveryOptions{HTTPClient: client, TrustedOrigin: trusted}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if TrustedOAuthAuthorizationServerOrigin("https://mcp.example.com", "http://127.0.0.1:4000") != "" {
		t.Fatal("loopback AS must not be trusted for a remote MCP server")
	}
}

func TestSelectOAuthAuthorizationServerRejectsPrivateMetadataAS(t *testing.T) {
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"resource":"https://resource.example.com","authorization_servers":["http://169.254.169.254/"]}`), nil
	})
	_, _, err := SelectOAuthAuthorizationServerURL(context.Background(), "https://resource.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "OAuth endpoint URL is not allowed: http://169.254.169.254/") {
		t.Fatalf("err = %v", err)
	}
}

// TS oauth.test.ts: "returns OAuth metadata when an origin issuer has a
// trailing slash" (809e922).
func TestOAuthASDiscoveryAcceptsOriginIssuerTrailingSlash(t *testing.T) {
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, strings.Replace(validOAuthASMetadata, `"issuer":"https://auth.example.com"`, `"issuer":"https://auth.example.com/"`, 1)), nil
	})
	metadata, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com/", OAuthDiscoveryOptions{HTTPClient: client})
	if err != nil || metadata == nil || metadata.Issuer != "https://auth.example.com/" {
		t.Fatalf("metadata=%v err=%v", metadata, err)
	}
}

func TestOAuthIssuerTrailingSlashOnlyForOriginIssuers(t *testing.T) {
	if !oauthIssuerMatches("https://auth.example.com/", "https://auth.example.com") {
		t.Fatal("origin issuer with trailing slash should match")
	}
	if oauthIssuerMatches("https://auth.example.com/tenant1/", "https://auth.example.com/tenant1") {
		t.Fatal("path issuer with trailing slash must not match")
	}
	if oauthIssuerMatches("https://evil.example/", "https://auth.example.com") {
		t.Fatal("different issuer must not match")
	}
}

// TS oauth-types d84ea43: metadata without code_challenge_methods_supported is
// accepted for OAuth metadata (the OIDC S256 check still applies).
func TestOAuthASDiscoveryAcceptsMetadataWithoutCodeChallengeMethods(t *testing.T) {
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"]}`), nil
	})
	metadata, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err != nil || metadata == nil {
		t.Fatalf("metadata=%v err=%v", metadata, err)
	}
}
