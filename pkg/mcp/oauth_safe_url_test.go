package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TS SafeUrlSchema rejects javascript:, data: and vbscript: URLs; a
// javascript:// URL has a host, so a scheme-and-host check alone accepts it.
func TestOAuthMetadataRejectsUnsafeURLSchemes(t *testing.T) {
	good := OAuthAuthorizationServerMetadata{
		Issuer:                 "https://as.example",
		AuthorizationEndpoint:  "https://as.example/authorize",
		TokenEndpoint:          "https://as.example/token",
		ResponseTypesSupported: []string{"code"},
	}
	if err := validateOAuthAuthorizationServerMetadata(good, "oauth"); err != nil {
		t.Fatalf("good metadata rejected: %v", err)
	}
	for _, bad := range []string{"javascript://as.example/%0aalert(1)", "JavaScript://as.example/x", "vbscript://as.example/x", "data://as.example/x"} {
		m := good
		m.AuthorizationEndpoint = bad
		if err := validateOAuthAuthorizationServerMetadata(m, "oauth"); err == nil {
			t.Errorf("authorization_endpoint %q accepted", bad)
		}
		m = good
		m.TokenEndpoint = bad
		if err := validateOAuthAuthorizationServerMetadata(m, "oauth"); err == nil {
			t.Errorf("token_endpoint %q accepted", bad)
		}
		m = good
		m.RegistrationEndpoint = bad
		if err := validateOAuthAuthorizationServerMetadata(m, "oauth"); err == nil {
			t.Errorf("registration_endpoint %q accepted", bad)
		}
		prm := OAuthProtectedResourceMetadata{Resource: "https://r.example", AuthorizationServers: []string{bad}}
		if err := validateOAuthProtectedResourceMetadata(prm); err == nil {
			t.Errorf("authorization server %q accepted", bad)
		}
	}
}

func TestDecodeOAuthTokenResponseRejectsUnsafeURLs(t *testing.T) {
	for _, field := range []string{"issuer", "authorization_server", "token_endpoint"} {
		for _, bad := range []string{"javascript://x/%0a", "data:text/plain,a", "not a url"} {
			body := `{"access_token":"a","token_type":"Bearer","` + field + `":"` + bad + `"}`
			resp := &http.Response{StatusCode: 200, Body: safeURLTestBody(body), Header: http.Header{}}
			if _, err := decodeOAuthTokenResponse(resp); err == nil {
				t.Errorf("%s=%q accepted", field, bad)
			}
		}
	}
	ok := &http.Response{StatusCode: 200, Header: http.Header{}, Body: safeURLTestBody(`{"access_token":"a","token_type":"Bearer","issuer":"https://as.example"}`)}
	if _, err := decodeOAuthTokenResponse(ok); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterOAuthClientRejectsUnsafeURLs(t *testing.T) {
	for _, field := range []string{"issuer", "authorization_server", "token_endpoint", "client_uri", "logo_uri", "tos_uri", "jwks_uri"} {
		client, _ := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
			return jsonResponse(http.StatusCreated, `{"client_id":"id","redirect_uris":["https://app.example.com/cb"],"`+field+`":"javascript://x/%0a"}`), nil
		})
		_, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", &OAuthAuthorizationServerMetadata{RegistrationEndpoint: "https://auth.example.com/register"}, OAuthClientMetadata{
			RedirectURIs: []string{"https://app.example.com/cb"},
		}, client)
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s: err = %v", field, err)
		}
	}
}

type safeURLTestReader struct{ *strings.Reader }

func (safeURLTestReader) Close() error { return nil }

func safeURLTestBody(s string) safeURLTestReader { return safeURLTestReader{strings.NewReader(s)} }
