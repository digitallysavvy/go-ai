package mcp

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestStartOAuthAuthorizationQueryOrderLikeURLSearchParams(t *testing.T) {
	res, _ := url.Parse("https://mcp.example.com/")
	got, _, err := StartOAuthAuthorization("https://auth.example.com", StartOAuthAuthorizationParams{
		Metadata: &OAuthAuthorizationServerMetadata{
			AuthorizationEndpoint:  "https://auth.example.com/authorize?tenant=a&client_id=old",
			ResponseTypesSupported: []string{"code"}, CodeChallengeMethodsSupported: []string{"S256"},
		},
		ClientInformation: OAuthClientInformation{ClientID: "cid"},
		RedirectURL:       "http://localhost:3000/cb?x=1",
		Scope:             "a b offline_access",
		State:             "st",
		Resource:          res,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got)
	var keys []string
	for _, part := range strings.Split(u.RawQuery, "&") {
		keys = append(keys, strings.SplitN(part, "=", 2)[0])
	}
	want := "tenant,client_id,response_type,code_challenge,code_challenge_method,redirect_uri,state,scope,prompt,resource"
	if strings.Join(keys, ",") != want {
		t.Fatalf("param order %v, want %s", keys, want)
	}
	if u.Query().Get("client_id") != "cid" || u.Query().Get("scope") != "a b offline_access" || u.Query().Get("resource") != "https://mcp.example.com" {
		t.Fatalf("values: %s", u.RawQuery)
	}
}

func TestOAuthEmptyArraysAccepted(t *testing.T) {
	m := OAuthAuthorizationServerMetadata{
		Issuer: "https://a", AuthorizationEndpoint: "https://a/authorize", TokenEndpoint: "https://a/token",
		ResponseTypesSupported: []string{},
	}
	if err := validateOAuthAuthorizationServerMetadata(m, "oauth"); err != nil {
		t.Fatalf("empty response_types_supported array is valid in TS: %v", err)
	}
	m.ResponseTypesSupported = nil
	if err := validateOAuthAuthorizationServerMetadata(m, "oauth"); err == nil {
		t.Fatal("missing response_types_supported must fail")
	}
}

func TestOAuthEmptyGrantTypesRejected(t *testing.T) {
	// JS arrays are truthy even when empty, so [] does not include the grant.
	_, err := RefreshOAuthAuthorization(t.Context(), "https://auth.example.com", RefreshOAuthAuthorizationParams{
		Metadata:     &OAuthAuthorizationServerMetadata{TokenEndpoint: "https://auth.example.com/token", GrantTypesSupported: []string{}},
		RefreshToken: "r",
	})
	if err == nil || !strings.Contains(err.Error(), "Incompatible auth server: does not support grant type refresh_token") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeOAuthTokenResponseRequiresAccessTokenAndType(t *testing.T) {
	for _, body := range []string{`{}`, `{"access_token":"a"}`, `{"token_type":"Bearer"}`, `[]`, `{"access_token":1,"token_type":"B"}`} {
		resp := jsonResponse(http.StatusOK, body)
		if _, err := decodeOAuthTokenResponse(resp); err == nil {
			t.Errorf("%s: expected error", body)
		}
	}
	if _, err := decodeOAuthTokenResponse(jsonResponse(http.StatusOK, `{"access_token":"a","token_type":"Bearer"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestSelectOAuthScopeEmptyPRMScopesFallBack(t *testing.T) {
	got := SelectOAuthScope("", &OAuthProtectedResourceMetadata{ScopesSupported: []string{""}}, OAuthClientMetadata{Scope: "client"})
	if got != "client" {
		t.Fatalf("got %q", got)
	}
}

func TestOAuthOriginNormalizesLikeURLOrigin(t *testing.T) {
	u, _ := url.Parse("HTTPS://Example.COM:443/x")
	if got := origin(u); got != "https://example.com" {
		t.Fatalf("got %s", got)
	}
	u, _ = url.Parse("http://[::1]:80/")
	if got := origin(u); got != "http://[::1]" {
		t.Fatalf("got %s", got)
	}
}

// TS parseErrorResponse maps unknown wire codes to ServerError, which auth()
// swallows on refresh before starting a fresh authorization.
func TestAuthRefreshUnknownOAuthErrorFallsThroughToRedirect(t *testing.T) {
	provider := newFakeOAuthProvider()
	provider.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
	provider.tokens = &OAuthTokens{
		AccessToken: "OLD", RefreshToken: "RT",
		Issuer: "https://auth.example.com", AuthorizationServer: "https://auth.example.com/",
		TokenEndpoint: "https://auth.example.com/token",
	}
	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusServiceUnavailable, `{"error":"temporarily_unavailable"}`), nil
		},
	})
	result, err := Auth(t.Context(), provider, AuthOptions{ServerURL: "https://auth.example.com/mcp", HTTPClient: client})
	if err != nil || result != AuthResultRedirect {
		t.Fatalf("result=%s err=%v, want REDIRECT", result, err)
	}
}
