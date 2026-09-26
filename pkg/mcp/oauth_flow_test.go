package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// TestGenerateOAuthPKCE mirrors the PKCE contract TS startAuthorization relies
// on from pkce-challenge: a URL-safe verifier and its S256 challenge.
func TestGenerateOAuthPKCE(t *testing.T) {
	verifier, challenge, err := GenerateOAuthPKCE()
	if err != nil {
		t.Fatalf("GenerateOAuthPKCE error: %v", err)
	}
	if len(verifier) < 43 {
		t.Fatalf("verifier length = %d, want >= 43", len(verifier))
	}
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	for _, r := range verifier {
		if !strings.ContainsRune(unreserved, r) {
			t.Fatalf("verifier contains disallowed character %q", r)
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Fatalf("challenge = %s, want S256(verifier) = %s", challenge, want)
	}

	// Two calls must not produce the same verifier.
	verifier2, _, err := GenerateOAuthPKCE()
	if err != nil {
		t.Fatalf("GenerateOAuthPKCE error: %v", err)
	}
	if verifier == verifier2 {
		t.Fatal("expected distinct verifiers across calls")
	}
}

// TestSelectOAuthScope mirrors TS selectScope precedence (oauth.ts, hash
// 1011e33): challenge scope > PRM scopes_supported > client metadata scope.
func TestSelectOAuthScope(t *testing.T) {
	clientMetadata := OAuthClientMetadata{Scope: "client:scope"}
	prm := &OAuthProtectedResourceMetadata{ScopesSupported: []string{"resource:read", "resource:write"}}

	if got := SelectOAuthScope("challenge:scope", prm, clientMetadata); got != "challenge:scope" {
		t.Fatalf("challenge scope precedence: got %q", got)
	}
	if got := SelectOAuthScope("", prm, clientMetadata); got != "resource:read resource:write" {
		t.Fatalf("PRM scope precedence: got %q", got)
	}
	if got := SelectOAuthScope("", nil, clientMetadata); got != "client:scope" {
		t.Fatalf("client metadata scope fallback: got %q", got)
	}
	if got := SelectOAuthScope("", &OAuthProtectedResourceMetadata{}, clientMetadata); got != "client:scope" {
		t.Fatalf("empty PRM scopes falls back to client metadata: got %q", got)
	}
}

// TestInferOAuthApplicationType mirrors TS inferOAuthApplicationType
// (oauth.ts, hash 1f29230).
func TestInferOAuthApplicationType(t *testing.T) {
	cases := []struct {
		name string
		uris []string
		want string
	}{
		{"loopback http", []string{"http://127.0.0.1:8080/callback"}, "native"},
		{"loopback https", []string{"https://localhost/callback"}, "native"},
		{"custom scheme", []string{"myapp://callback"}, "native"},
		{"mixed loopback and custom scheme", []string{"http://127.0.0.1/cb", "myapp://cb"}, "native"},
		{"public https", []string{"https://app.example.com/callback"}, "web"},
		{"mixed native and web", []string{"http://127.0.0.1/cb", "https://app.example.com/cb"}, "web"},
		{"empty", []string{}, "native"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferOAuthApplicationType(tc.uris); got != tc.want {
				t.Fatalf("inferOAuthApplicationType(%v) = %q, want %q", tc.uris, got, tc.want)
			}
		})
	}
}

// TestSelectOAuthClientAuthMethod mirrors TS selectClientAuthMethod
// (oauth.ts).
func TestSelectOAuthClientAuthMethod(t *testing.T) {
	withSecret := OAuthClientInformation{ClientID: "id", ClientSecret: "secret"}
	public := OAuthClientInformation{ClientID: "id"}

	cases := []struct {
		name      string
		info      OAuthClientInformation
		supported []string
		want      oauthClientAuthMethod
	}{
		{"no supported methods, has secret -> post", withSecret, nil, oauthClientAuthPost},
		{"no supported methods, public -> none", public, nil, oauthClientAuthNone},
		{"prefers basic when supported and has secret", withSecret, []string{"client_secret_post", "client_secret_basic"}, oauthClientAuthBasic},
		{"falls back to post when basic unsupported", withSecret, []string{"client_secret_post"}, oauthClientAuthPost},
		{"public client uses none when supported", public, []string{"client_secret_basic", "none"}, oauthClientAuthNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectOAuthClientAuthMethod(tc.info, tc.supported); got != tc.want {
				t.Fatalf("selectOAuthClientAuthMethod() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyOAuthClientAuthentication(t *testing.T) {
	info := OAuthClientInformation{ClientID: "cid", ClientSecret: "csecret"}

	t.Run("basic", func(t *testing.T) {
		headers := http.Header{}
		params := url.Values{}
		if err := applyOAuthClientAuthentication(oauthClientAuthBasic, info, headers, params); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("cid:csecret"))
		if headers.Get("Authorization") != want {
			t.Fatalf("Authorization header = %q, want %q", headers.Get("Authorization"), want)
		}
	})

	t.Run("basic requires secret", func(t *testing.T) {
		if err := applyOAuthClientAuthentication(oauthClientAuthBasic, OAuthClientInformation{ClientID: "cid"}, http.Header{}, url.Values{}); err == nil {
			t.Fatal("expected error when client_secret_basic used without a secret")
		}
	})

	t.Run("post", func(t *testing.T) {
		params := url.Values{}
		if err := applyOAuthClientAuthentication(oauthClientAuthPost, info, http.Header{}, params); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.Get("client_id") != "cid" || params.Get("client_secret") != "csecret" {
			t.Fatalf("params = %v", params)
		}
	})

	t.Run("none", func(t *testing.T) {
		params := url.Values{}
		if err := applyOAuthClientAuthentication(oauthClientAuthNone, info, http.Header{}, params); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.Get("client_id") != "cid" || params.Has("client_secret") {
			t.Fatalf("params = %v", params)
		}
	})
}

// TestStartOAuthAuthorization mirrors TS startAuthorization (oauth.ts).
func TestStartOAuthAuthorization(t *testing.T) {
	metadata := &OAuthAuthorizationServerMetadata{
		AuthorizationEndpoint:         "https://auth.example.com/authorize",
		ResponseTypesSupported:        []string{"code"},
		CodeChallengeMethodsSupported: []string{"S256"},
	}
	clientInfo := OAuthClientInformation{ClientID: "client-123"}
	resource, _ := url.Parse("https://api.example.com/mcp")

	authURL, verifier, err := StartOAuthAuthorization("https://auth.example.com", StartOAuthAuthorizationParams{
		Metadata:          metadata,
		ClientInformation: clientInfo,
		RedirectURL:       "https://app.example.com/callback",
		Scope:             "mcp:read offline_access",
		State:             "state-xyz",
		Resource:          resource,
	})
	if err != nil {
		t.Fatalf("StartOAuthAuthorization error: %v", err)
	}
	if verifier == "" {
		t.Fatal("expected a non-empty code verifier")
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("invalid authorization URL: %v", err)
	}
	q := parsed.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != "client-123" || q.Get("code_challenge_method") != "S256" ||
		q.Get("redirect_uri") != "https://app.example.com/callback" || q.Get("state") != "state-xyz" {
		t.Fatalf("authorization URL query = %v", q)
	}
	if q.Get("scope") != "mcp:read offline_access" {
		t.Fatalf("scope = %q", q.Get("scope"))
	}
	if q.Get("prompt") != "consent" {
		t.Fatal("expected prompt=consent for offline_access scope")
	}
	if q.Get("resource") != "https://api.example.com/mcp" {
		t.Fatalf("resource = %q", q.Get("resource"))
	}
	if q.Get("code_challenge") == "" {
		t.Fatal("expected a code_challenge parameter")
	}

	t.Run("rejects unsupported response type", func(t *testing.T) {
		_, _, err := StartOAuthAuthorization("https://auth.example.com", StartOAuthAuthorizationParams{
			Metadata:          &OAuthAuthorizationServerMetadata{AuthorizationEndpoint: "https://auth.example.com/authorize", ResponseTypesSupported: []string{"token"}},
			ClientInformation: clientInfo,
			RedirectURL:       "https://app.example.com/callback",
		})
		if err == nil || !strings.Contains(err.Error(), "response type") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("rejects unsupported code challenge method", func(t *testing.T) {
		_, _, err := StartOAuthAuthorization("https://auth.example.com", StartOAuthAuthorizationParams{
			Metadata:          &OAuthAuthorizationServerMetadata{AuthorizationEndpoint: "https://auth.example.com/authorize", ResponseTypesSupported: []string{"code"}, CodeChallengeMethodsSupported: []string{"plain"}},
			ClientInformation: clientInfo,
			RedirectURL:       "https://app.example.com/callback",
		})
		if err == nil || !strings.Contains(err.Error(), "code challenge method") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("defaults to /authorize when metadata absent", func(t *testing.T) {
		authURL, _, err := StartOAuthAuthorization("https://auth.example.com", StartOAuthAuthorizationParams{
			ClientInformation: clientInfo,
			RedirectURL:       "https://app.example.com/callback",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(authURL, "https://auth.example.com/authorize?") {
			t.Fatalf("authURL = %s", authURL)
		}
	})
}

// oauthCapturedRequest records one intercepted HTTP request's method, URL,
// headers, and decoded form/JSON body for assertions.
type oauthCapturedRequest struct {
	method  string
	url     string
	headers http.Header
	form    url.Values
	json    map[string]interface{}
}

func newOAuthCapturingClient(t *testing.T, handler func(req *oauthCapturedRequest) (*http.Response, error)) (*http.Client, *[]oauthCapturedRequest) {
	t.Helper()
	var mu sync.Mutex
	var captured []oauthCapturedRequest
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		record := oauthCapturedRequest{method: req.Method, url: req.URL.String(), headers: req.Header.Clone()}
		if req.Body != nil {
			bodyBytes := make([]byte, 0)
			buf := make([]byte, 4096)
			for {
				n, err := req.Body.Read(buf)
				bodyBytes = append(bodyBytes, buf[:n]...)
				if err != nil {
					break
				}
			}
			contentType := req.Header.Get("Content-Type")
			if strings.Contains(contentType, "json") {
				_ = json.Unmarshal(bodyBytes, &record.json)
			} else {
				record.form, _ = url.ParseQuery(string(bodyBytes))
			}
		}
		mu.Lock()
		captured = append(captured, record)
		mu.Unlock()
		return handler(&record)
	})
	return client, &captured
}

func TestExchangeOAuthAuthorization(t *testing.T) {
	tokenResponse := `{"access_token":"AT","token_type":"Bearer","expires_in":3600,"refresh_token":"RT"}`
	client, captured := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		return jsonResponse(http.StatusOK, tokenResponse), nil
	})

	resource, _ := url.Parse("https://api.example.com/mcp/")
	tokens, err := ExchangeOAuthAuthorization(context.Background(), "https://auth.example.com", ExchangeOAuthAuthorizationParams{
		Metadata:          &OAuthAuthorizationServerMetadata{TokenEndpoint: "https://auth.example.com/token"},
		ClientInformation: OAuthClientInformation{ClientID: "cid", ClientSecret: "csecret"},
		AuthorizationCode: "auth-code",
		CodeVerifier:      "verifier-abc",
		RedirectURI:       "https://app.example.com/callback",
		Resource:          resource,
		HTTPClient:        client,
	})
	if err != nil {
		t.Fatalf("ExchangeOAuthAuthorization error: %v", err)
	}
	if tokens.AccessToken != "AT" || tokens.RefreshToken != "RT" {
		t.Fatalf("tokens = %#v", tokens)
	}

	req := (*captured)[0]
	if req.method != http.MethodPost || req.url != "https://auth.example.com/token" {
		t.Fatalf("request = %+v", req)
	}
	if req.form.Get("grant_type") != "authorization_code" || req.form.Get("code") != "auth-code" ||
		req.form.Get("code_verifier") != "verifier-abc" || req.form.Get("redirect_uri") != "https://app.example.com/callback" {
		t.Fatalf("form = %v", req.form)
	}
	// No token_endpoint_auth_methods_supported -> defaults to client_secret_post.
	if req.form.Get("client_id") != "cid" || req.form.Get("client_secret") != "csecret" {
		t.Fatalf("client auth not applied: %v", req.form)
	}
	// Resource had its trailing slash stripped (hash 1e89d62) — no, this
	// resource has a path, so no slash to strip; assert it round-trips as-is.
	if req.form.Get("resource") != "https://api.example.com/mcp/" {
		t.Fatalf("resource = %q", req.form.Get("resource"))
	}
}

// TestExchangeOAuthAuthorizationStripsPathlessResourceSlash mirrors TS
// resourceUrlStripSlash (util/oauth-util.ts, hash 1e89d62).
func TestExchangeOAuthAuthorizationStripsPathlessResourceSlash(t *testing.T) {
	client, captured := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"access_token":"AT","token_type":"Bearer"}`), nil
	})
	resource, _ := url.Parse("https://api.example.com/")
	_, err := ExchangeOAuthAuthorization(context.Background(), "https://auth.example.com", ExchangeOAuthAuthorizationParams{
		Metadata:          &OAuthAuthorizationServerMetadata{TokenEndpoint: "https://auth.example.com/token"},
		ClientInformation: OAuthClientInformation{ClientID: "cid"},
		AuthorizationCode: "code",
		CodeVerifier:      "verifier",
		RedirectURI:       "https://app.example.com/cb",
		Resource:          resource,
		HTTPClient:        client,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if (*captured)[0].form.Get("resource") != "https://api.example.com" {
		t.Fatalf("resource = %q, want trailing slash stripped", (*captured)[0].form.Get("resource"))
	}
}

func TestExchangeOAuthAuthorizationErrorResponse(t *testing.T) {
	client, _ := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		return jsonResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"code expired"}`), nil
	})
	_, err := ExchangeOAuthAuthorization(context.Background(), "https://auth.example.com", ExchangeOAuthAuthorizationParams{
		Metadata:          &OAuthAuthorizationServerMetadata{TokenEndpoint: "https://auth.example.com/token"},
		ClientInformation: OAuthClientInformation{ClientID: "cid"},
		AuthorizationCode: "code",
		CodeVerifier:      "verifier",
		RedirectURI:       "https://app.example.com/cb",
		HTTPClient:        client,
	})
	if err == nil || !IsInvalidGrantError(err) {
		t.Fatalf("err = %v, want IsInvalidGrantError", err)
	}
}

func TestExchangeOAuthAuthorizationRejectsPrivateTokenEndpoint(t *testing.T) {
	client, _ := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		t.Fatal("request should have been rejected before it was sent")
		return nil, nil
	})
	_, err := ExchangeOAuthAuthorization(context.Background(), "https://auth.example.com", ExchangeOAuthAuthorizationParams{
		Metadata:          &OAuthAuthorizationServerMetadata{TokenEndpoint: "http://169.254.169.254/token"},
		ClientInformation: OAuthClientInformation{ClientID: "cid"},
		AuthorizationCode: "code",
		CodeVerifier:      "verifier",
		RedirectURI:       "https://app.example.com/cb",
		HTTPClient:        client,
	})
	if err == nil {
		t.Fatal("expected SSRF guard error for link-local token endpoint")
	}
}

// TestRefreshOAuthAuthorizationPreservesRefreshToken mirrors TS
// refreshAuthorization's "preserves the original refresh token if a new one
// is not returned".
func TestRefreshOAuthAuthorizationPreservesRefreshToken(t *testing.T) {
	client, captured := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"access_token":"NEW_AT","token_type":"Bearer"}`), nil
	})
	tokens, err := RefreshOAuthAuthorization(context.Background(), "https://auth.example.com", RefreshOAuthAuthorizationParams{
		Metadata:          &OAuthAuthorizationServerMetadata{TokenEndpoint: "https://auth.example.com/token", GrantTypesSupported: []string{"refresh_token"}},
		ClientInformation: OAuthClientInformation{ClientID: "cid"},
		RefreshToken:      "original-refresh-token",
		HTTPClient:        client,
	})
	if err != nil {
		t.Fatalf("RefreshOAuthAuthorization error: %v", err)
	}
	if tokens.AccessToken != "NEW_AT" || tokens.RefreshToken != "original-refresh-token" {
		t.Fatalf("tokens = %#v, want original refresh token preserved", tokens)
	}
	if (*captured)[0].form.Get("grant_type") != "refresh_token" || (*captured)[0].form.Get("refresh_token") != "original-refresh-token" {
		t.Fatalf("form = %v", (*captured)[0].form)
	}
}

// TestRefreshOAuthAuthorizationRejectsPrivateTokenEndpoint mirrors TS
// refreshAuthorization's "rejects private token endpoints before sending the
// refresh token" (oauth.test.ts): the SSRF guard must reject a link-local
// token endpoint before the refresh token is ever sent over the wire.
func TestRefreshOAuthAuthorizationRejectsPrivateTokenEndpoint(t *testing.T) {
	client, _ := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		t.Fatal("request should have been rejected before it was sent")
		return nil, nil
	})
	_, err := RefreshOAuthAuthorization(context.Background(), "https://attacker.example", RefreshOAuthAuthorizationParams{
		Metadata:          &OAuthAuthorizationServerMetadata{TokenEndpoint: "http://169.254.169.254/latest/token"},
		ClientInformation: OAuthClientInformation{ClientID: "cid"},
		RefreshToken:      "real-refresh-token",
		HTTPClient:        client,
	})
	if err == nil {
		t.Fatal("expected SSRF guard error for link-local token endpoint")
	}
	if !strings.Contains(err.Error(), "OAuth endpoint URL is not allowed") {
		t.Fatalf("err = %v, want SSRF guard message", err)
	}
}

func TestRefreshOAuthAuthorizationUnsupportedGrantType(t *testing.T) {
	_, err := RefreshOAuthAuthorization(context.Background(), "https://auth.example.com", RefreshOAuthAuthorizationParams{
		Metadata:          &OAuthAuthorizationServerMetadata{TokenEndpoint: "https://auth.example.com/token", GrantTypesSupported: []string{"authorization_code"}},
		ClientInformation: OAuthClientInformation{ClientID: "cid"},
		RefreshToken:      "rt",
	})
	if err == nil || !strings.Contains(err.Error(), "grant type") {
		t.Fatalf("err = %v", err)
	}
}

// TestRegisterOAuthClientIncludesScopeAndApplicationType mirrors TS
// registerClient scope selection (hash bf591f0) and application_type
// inference (hash 1f29230).
func TestRegisterOAuthClientIncludesScopeAndApplicationType(t *testing.T) {
	client, captured := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, `{"client_id":"new-client-id","redirect_uris":["http://127.0.0.1/cb"]}`), nil
	})

	full, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", &OAuthAuthorizationServerMetadata{RegistrationEndpoint: "https://auth.example.com/register"}, OAuthClientMetadata{
		RedirectURIs: []string{"http://127.0.0.1/cb"},
		Scope:        "mcp:read mcp:write",
	}, client)
	if err != nil {
		t.Fatalf("RegisterOAuthClient error: %v", err)
	}
	if full.ClientID != "new-client-id" {
		t.Fatalf("full = %#v", full)
	}

	req := (*captured)[0]
	if req.json["scope"] != "mcp:read mcp:write" {
		t.Fatalf("registration body scope = %v", req.json["scope"])
	}
	if req.json["application_type"] != "native" {
		t.Fatalf("registration body application_type = %v, want native for loopback redirect URI", req.json["application_type"])
	}
}

func TestRegisterOAuthClientRejectsPrivateRegistrationEndpoint(t *testing.T) {
	client, _ := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		t.Fatal("request should have been rejected before it was sent")
		return nil, nil
	})
	_, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", &OAuthAuthorizationServerMetadata{RegistrationEndpoint: "http://169.254.169.254/register"}, OAuthClientMetadata{
		RedirectURIs: []string{"https://app.example.com/cb"},
	}, client)
	if err == nil {
		t.Fatal("expected SSRF guard error for link-local registration endpoint")
	}
}

func TestRegisterOAuthClientRequiresRegistrationEndpoint(t *testing.T) {
	_, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", &OAuthAuthorizationServerMetadata{}, OAuthClientMetadata{
		RedirectURIs: []string{"https://app.example.com/cb"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "dynamic client registration") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseOAuthErrorResponse(t *testing.T) {
	t.Run("well-formed error", func(t *testing.T) {
		resp := jsonResponse(http.StatusBadRequest, `{"error":"invalid_client","error_description":"unknown client"}`)
		err := ParseOAuthErrorResponse(resp)
		if err.Code != OAuthErrorCodeInvalidClient || err.Message != "unknown client" {
			t.Fatalf("err = %#v", err)
		}
		if !IsInvalidClientError(err) {
			t.Fatal("expected IsInvalidClientError")
		}
	})

	t.Run("malformed body falls back to server_error", func(t *testing.T) {
		resp := jsonResponse(http.StatusInternalServerError, `not json`)
		err := ParseOAuthErrorResponse(resp)
		if err.Code != OAuthErrorCodeServerError || !strings.Contains(err.Message, "500") {
			t.Fatalf("err = %#v", err)
		}
	})
}
