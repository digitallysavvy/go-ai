package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TS 88b95757c1 oauth-discovery-runtime.test.ts "%s endpoints": registration,
// code exchange and refresh accept a developer-configured trustedOrigin.
// An endpoint on that origin is allowed (here a loopback one, which the SSRF
// guard otherwise rejects for a non-loopback authorization server) and an
// endpoint off it is still guarded.
func TestR91toolsOAuthEndpointsTrustedOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_id":"example-client","redirect_uris":["https://app.example.com/callback"],"access_token":"example-token","token_type":"Bearer"}`))
	}))
	t.Cleanup(srv.Close)
	const issuer = "https://auth.example.com"
	metadata := &OAuthAuthorizationServerMetadata{
		Issuer: issuer, AuthorizationEndpoint: issuer + "/authorize",
		TokenEndpoint: srv.URL + "/token", RegistrationEndpoint: srv.URL + "/register",
		ResponseTypesSupported: []string{"code"},
	}
	client := OAuthClientInformation{ClientID: "example-client"}
	ops := map[string]func(trusted string) error{
		"registration": func(trusted string) error {
			_, err := RegisterOAuthClientWithOptions(context.Background(), issuer, RegisterOAuthClientOptions{
				Metadata: metadata, ClientMetadata: OAuthClientMetadata{RedirectURIs: []string{"https://app.example.com/callback"}}, TrustedOrigin: trusted,
			})
			return err
		},
		"code exchange": func(trusted string) error {
			_, err := ExchangeOAuthAuthorization(context.Background(), issuer, ExchangeOAuthAuthorizationParams{
				Metadata: metadata, ClientInformation: client, AuthorizationCode: "unused", CodeVerifier: "unused",
				RedirectURI: "https://app.example.com/callback", TrustedOrigin: trusted,
			})
			return err
		},
		"refresh": func(trusted string) error {
			_, err := RefreshOAuthAuthorization(context.Background(), issuer, RefreshOAuthAuthorizationParams{
				Metadata: metadata, ClientInformation: client, RefreshToken: "unused", TrustedOrigin: trusted,
			})
			return err
		},
	}
	for name, op := range ops {
		if err := op(""); err == nil || !strings.Contains(err.Error(), "OAuth endpoint URL is not allowed") {
			t.Errorf("%s without trustedOrigin: err = %v, want the SSRF guard", name, err)
		}
		if err := op(srv.URL); err != nil {
			t.Errorf("%s with trustedOrigin: %v", name, err)
		}
		if err := op("https://other.example.com"); err == nil {
			t.Errorf("%s with another trustedOrigin: want the SSRF guard", name)
		}
	}
}
