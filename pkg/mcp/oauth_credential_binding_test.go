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

// TS #23579: OAuth client credentials are bound to their authorization server.

func bindingFlowClient(t *testing.T, posts *[]string) *http.Client {
	t.Helper()
	var mu sync.Mutex
	return oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://api.example.com/mcp-server","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			*posts = append(*posts, req.URL.String())
			mu.Unlock()
			return jsonResponse(http.StatusOK, `{"access_token":"AT","token_type":"Bearer"}`), nil
		},
	})
}

func isASMismatch(err error) bool {
	var oe *MCPClientOAuthError
	return errors.As(err, &oe) && oe.Code == OAuthErrorCodeAuthorizationServerMismatch
}

func TestAuthRejectsAttackerSelectedAuthorizationServer(t *testing.T) {
	bindings := map[string]OAuthClientInformation{
		"unbound credentials":                      {},
		"an issuer without endpoint metadata":      {Issuer: "https://legitimate-as.example"},
		"endpoint metadata without an issuer":      {AuthorizationServer: "https://legitimate-as.example", TokenEndpoint: "https://legitimate-as.example/token"},
		"matching issuer, mismatching endpoint AS": {Issuer: "https://auth.example.com", AuthorizationServer: "https://legitimate-as.example", TokenEndpoint: "https://legitimate-as.example/token"},
	}
	for name, binding := range bindings {
		t.Run(name, func(t *testing.T) {
			var posts []string
			provider := newFakeOAuthProvider()
			info := binding
			info.ClientID = "legitimate-client"
			info.ClientSecret = "secret-registered-with-legitimate-as"
			provider.clientInfo = &info

			_, err := Auth(context.Background(), provider, AuthOptions{ServerURL: "https://api.example.com/mcp-server", HTTPClient: bindingFlowClient(t, &posts)})
			if !isASMismatch(err) {
				t.Fatalf("err = %v, want authorization server mismatch", err)
			}
			if provider.asInfo != nil || len(provider.redirectedTo) != 0 || len(posts) != 0 {
				t.Fatalf("pinned=%v redirects=%v posts=%v, want none", provider.asInfo, provider.redirectedTo, posts)
			}
		})
	}
}

func TestAuthRejectsUnboundCredentialsWithCustomAuthentication(t *testing.T) {
	var posts []string
	base := newFakeOAuthProvider()
	base.clientInfo = &OAuthClientInformation{ClientID: "legitimate-client"}
	called := false
	provider := &authenticatingOAuthProvider{
		fakeOAuthProvider: base,
		addClientAuthentication: func(context.Context, http.Header, url.Values, string, *OAuthAuthorizationServerMetadata) error {
			called = true
			return nil
		},
	}
	_, err := Auth(context.Background(), provider, AuthOptions{ServerURL: "https://api.example.com/mcp-server", HTTPClient: bindingFlowClient(t, &posts)})
	if !isASMismatch(err) || called || base.asInfo != nil || len(base.redirectedTo) != 0 || len(posts) != 0 {
		t.Fatalf("err=%v called=%v pin=%v redirects=%v posts=%v", err, called, base.asInfo, base.redirectedTo, posts)
	}
}

func TestAuthStoredFlowPinDoesNotOverrideCredentialIssuer(t *testing.T) {
	var posts []string
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "legitimate-client", ClientSecret: "legitimate-secret", Issuer: "https://legitimate-as.example"}
	provider.asInfo = &OAuthAuthorizationServerInformation{
		Issuer:                 "https://auth.example.com",
		AuthorizationServerURL: "https://auth.example.com/",
		TokenEndpoint:          "https://auth.example.com/token",
	}
	provider.codeVerifier = "v"
	_, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL: "https://api.example.com/mcp-server", HasAuthorizationCode: true,
		AuthorizationCode: "attacker-supplied-code", HTTPClient: bindingFlowClient(t, &posts),
	})
	if !isASMismatch(err) || len(posts) != 0 {
		t.Fatalf("err=%v posts=%v", err, posts)
	}
}

func TestAuthExchangesCredentialsBoundOnlyByIssuer(t *testing.T) {
	for _, method := range []string{"client_secret_basic", "client_secret_post"} {
		t.Run(method, func(t *testing.T) {
			var gotAuth, gotSecret string
			metadata := strings.Replace(oauthFlowASMetadataJSON, `"grant_types_supported"`, `"token_endpoint_auth_methods_supported": ["`+method+`"], "grant_types_supported"`, 1)
			client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
				"/.well-known/oauth-protected-resource": func(*http.Request) (*http.Response, error) {
					return jsonResponse(http.StatusOK, `{"resource":"https://api.example.com/mcp-server","authorization_servers":["https://auth.example.com"]}`), nil
				},
				"/.well-known/oauth-authorization-server": func(*http.Request) (*http.Response, error) {
					return jsonResponse(http.StatusOK, metadata), nil
				},
				"/token": func(req *http.Request) (*http.Response, error) {
					gotAuth = req.Header.Get("Authorization")
					body, _ := io.ReadAll(req.Body)
					values, _ := url.ParseQuery(string(body))
					gotSecret = values.Get("client_secret")
					return jsonResponse(http.StatusOK, `{"access_token":"AT","token_type":"Bearer"}`), nil
				},
			})
			provider := newFakeOAuthProvider()
			provider.clientInfo = &OAuthClientInformation{ClientID: "test-client", ClientSecret: "test-secret", Issuer: "https://auth.example.com"}
			opts := AuthOptions{ServerURL: "https://api.example.com/mcp-server", HTTPClient: client}

			if res, err := Auth(context.Background(), provider, opts); err != nil || res != AuthResultRedirect {
				t.Fatalf("first: %v %v", res, err)
			}
			opts.HasAuthorizationCode, opts.AuthorizationCode = true, "test-code"
			if res, err := Auth(context.Background(), provider, opts); err != nil || res != AuthResultAuthorized {
				t.Fatalf("second: %v %v", res, err)
			}
			if method == "client_secret_basic" && gotAuth == "" || method == "client_secret_post" && gotSecret != "test-secret" {
				t.Fatalf("auth=%q secret=%q", gotAuth, gotSecret)
			}
		})
	}
}

// After invalid_grant removes the rejected tokens, their binding still
// guards the retry (even when the server changes between attempts).
func TestAuthRetainsTokenOnlyBindingAfterInvalidGrant(t *testing.T) {
	for _, changeServer := range []bool{false, true} {
		name := "same server"
		if changeServer {
			name = "server changes on retry"
		}
		t.Run(name, func(t *testing.T) {
			const issuer = "https://auth.example.com"
			selected := issuer
			var mu sync.Mutex
			var posts []string
			provider := newFakeOAuthProvider()
			provider.clientInfo = &OAuthClientInformation{ClientID: "pre-registered-client", ClientSecret: "client-secret"}
			provider.tokens = &OAuthTokens{
				AccessToken: "expired", RefreshToken: "rejected", TokenType: "Bearer",
				Issuer: issuer, AuthorizationServer: issuer, TokenEndpoint: issuer + "/token",
			}
			client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				p := req.URL.Path
				switch {
				case strings.Contains(p, "/.well-known/oauth-protected-resource"):
					return jsonResponse(200, `{"resource":"https://api.example.com/mcp-server","authorization_servers":["`+selected+`"]}`), nil
				case strings.Contains(p, "/.well-known/oauth-authorization-server"):
					return jsonResponse(200, `{"issuer":"`+selected+`","authorization_endpoint":"`+selected+`/authorize","token_endpoint":"`+selected+`/token","response_types_supported":["code"],"code_challenge_methods_supported":["S256"]}`), nil
				case req.Method == http.MethodPost:
					posts = append(posts, req.URL.String())
					if changeServer {
						selected = "https://attacker.example"
					}
					return jsonResponse(400, `{"error":"invalid_grant"}`), nil
				}
				return jsonResponse(404, `{}`), nil
			})
			res, err := Auth(context.Background(), provider, AuthOptions{ServerURL: "https://api.example.com/mcp-server", HTTPClient: client})
			if len(posts) != 1 || posts[0] != issuer+"/token" {
				t.Fatalf("posts = %v", posts)
			}
			if provider.tokens != nil {
				t.Fatalf("rejected tokens were not invalidated")
			}
			if changeServer {
				if !isASMismatch(err) || len(provider.redirectedTo) != 0 || provider.asInfo != nil {
					t.Fatalf("err=%v redirects=%v pin=%v", err, provider.redirectedTo, provider.asInfo)
				}
			} else if err != nil || res != AuthResultRedirect || len(provider.redirectedTo) != 1 || provider.asInfo == nil {
				t.Fatalf("res=%v err=%v redirects=%v pin=%v", res, err, provider.redirectedTo, provider.asInfo)
			}
		})
	}
}
