package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

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
