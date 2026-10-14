package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// TS #22055: a rejected authorization code (invalid_grant) must not
// invalidate stored credentials nor be retried.
func TestAuthRejectedAuthorizationCodePreservesCredentials(t *testing.T) {
	existing := &OAuthTokens{AccessToken: "old", RefreshToken: "old-r", TokenType: "Bearer"}
	for _, tc := range []struct {
		name   string
		tokens *OAuthTokens
	}{{"existing", existing}, {"absent", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			provider := newFakeOAuthProvider()
			provider.tokens = tc.tokens
			provider.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
			provider.codeVerifier = "verifier"
			provider.asInfo = &OAuthAuthorizationServerInformation{
				Issuer:                 "https://auth.example.com",
				AuthorizationServerURL: "https://auth.example.com/",
				TokenEndpoint:          "https://auth.example.com/token",
			}
			var mu sync.Mutex
			tokenCalls := 0
			client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
				"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
					return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
				},
				"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
					return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
				},
				"/token": func(req *http.Request) (*http.Response, error) {
					mu.Lock()
					tokenCalls++
					mu.Unlock()
					return jsonResponse(http.StatusBadRequest, `{"error":"invalid_grant"}`), nil
				},
			})
			_, err := Auth(context.Background(), provider, AuthOptions{
				ServerURL:            "https://auth.example.com/mcp",
				HasAuthorizationCode: true,
				AuthorizationCode:    "rejected-code",
				HTTPClient:           client,
			})
			if !IsInvalidGrantError(err) {
				t.Fatalf("err = %v, want invalid_grant", err)
			}
			if provider.tokens != tc.tokens {
				t.Fatalf("tokens changed: %#v", provider.tokens)
			}
			if len(provider.invalidatedLog) != 0 || len(provider.savedTokens) != 0 || len(provider.redirectedTo) != 0 {
				t.Fatalf("unexpected side effects: %v %v %v", provider.invalidatedLog, provider.savedTokens, provider.redirectedTo)
			}
			if tokenCalls != 1 {
				t.Fatalf("token endpoint calls = %d, want 1", tokenCalls)
			}
		})
	}
}

// TS #22248: authorization server paths beginning with repeated slashes must
// not change the discovery origin.
func TestBuildAuthorizationServerDiscoveryURLsPreservesOrigin(t *testing.T) {
	for _, path := range []string{
		"", "/tenant1", "/tenant1/", "//evil.example", "//evil.example/",
		"///evil.example/tenant", "//user@evil.example:8443", "/%2F%2Fevil.example",
		"/\\evil.example", "//",
	} {
		issuer := "https://auth.example.com:8443" + path
		urls, err := BuildAuthorizationServerDiscoveryURLs(issuer)
		if err != nil {
			continue // unparseable issuers are rejected outright
		}
		for _, u := range urls {
			parsed, perr := url.Parse(u.URL)
			if perr != nil || parsed.Scheme != "https" || parsed.Host != "auth.example.com:8443" {
				t.Fatalf("path %q: candidate %q escapes origin", path, u.URL)
			}
		}
		if path == "//evil.example" {
			if got := urls[len(urls)-1].URL; got != "https://auth.example.com:8443//evil.example/.well-known/openid-configuration" {
				t.Fatalf("last candidate = %s", got)
			}
		}
	}
}

func TestDiscoverAuthorizationServerMetadataDoubleSlashIssuerStaysOnOrigin(t *testing.T) {
	issuer := "https://auth.example.com//evil.example"
	metadata := `{"issuer":"` + issuer + `","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"],"code_challenge_methods_supported":["S256"],"jwks_uri":"https://auth.example.com/jwks","subject_types_supported":["public"],"id_token_signing_alg_values_supported":["RS256"]}`
	var mu sync.Mutex
	var seen []string
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		seen = append(seen, req.URL.String())
		mu.Unlock()
		if req.URL.Host != "auth.example.com" {
			t.Errorf("request escaped origin: %s", req.URL)
			return jsonResponse(http.StatusOK, strings.ReplaceAll(metadata, "auth.example.com/token", "evil.example/steal")), nil
		}
		if req.URL.String() == "https://auth.example.com//evil.example/.well-known/openid-configuration" {
			return jsonResponse(http.StatusOK, metadata), nil
		}
		return jsonResponse(http.StatusNotFound, `{}`), nil
	})
	got, err := DiscoverAuthorizationServerMetadata(context.Background(), issuer, OAuthDiscoveryOptions{HTTPClient: client})
	if err != nil || got == nil || got.TokenEndpoint != "https://auth.example.com/token" {
		t.Fatalf("got %#v, err %v", got, err)
	}
	want := []string{
		"https://auth.example.com/.well-known/oauth-authorization-server//evil.example",
		"https://auth.example.com/.well-known/oauth-authorization-server",
		"https://auth.example.com/.well-known/openid-configuration//evil.example",
		"https://auth.example.com//evil.example/.well-known/openid-configuration",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %v", seen)
	}
}
