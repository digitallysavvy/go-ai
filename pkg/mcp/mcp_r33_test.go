package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// reporterFailingProvider fails when asked whether the client was dynamically
// registered, and counts the questions.
type reporterFailingProvider struct {
	*fakeOAuthProvider
	asked int
}

func (p *reporterFailingProvider) IsClientInformationDynamicallyRegistered(context.Context) (bool, error) {
	p.asked++
	return false, errors.New("reporter unavailable")
}

// TS auth() checks `authorizationCode !== undefined ||
// !(await provider.isClientInformationDynamicallyRegistered?.())`: with an
// authorization code the original invalid_client error is thrown and the
// provider is never asked.
func TestAuthCodeExchangeInvalidClientDoesNotAskDynamicRegistration(t *testing.T) {
	base := newFakeOAuthProvider()
	base.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
	base.codeVerifier = "verifier-xyz"
	base.asInfo = &OAuthAuthorizationServerInformation{
		Issuer:                 "https://auth.example.com",
		AuthorizationServerURL: "https://auth.example.com/",
		TokenEndpoint:          "https://auth.example.com/token",
	}
	provider := &reporterFailingProvider{fakeOAuthProvider: base}
	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusBadRequest, `{"error":"invalid_client","error_description":"unknown client"}`), nil
		},
	})
	_, err := Auth(context.Background(), provider, AuthOptions{
		ServerURL:            "https://auth.example.com/mcp",
		HasAuthorizationCode: true,
		AuthorizationCode:    "the-code",
		HTTPClient:           client,
	})
	if err == nil || !IsInvalidClientError(err) {
		t.Fatalf("err = %v, want the invalid_client error", err)
	}
	if provider.asked != 0 {
		t.Fatalf("provider asked %d times about dynamic registration, want 0", provider.asked)
	}
}

// TS prints a missing mimeType as "undefined" and lets atob throw on a
// malformed blob.
func TestMCPAppResourceErrorTextsMatchTS(t *testing.T) {
	_, err := GetMCPAppResourceFromReadResult("ui://a", ReadResourceResult{Contents: []ResourceContent{{URI: "ui://a", Text: "x"}}})
	if err == nil || err.Error() != "Unsupported MCP App resource MIME type: undefined" {
		t.Fatalf("missing mimeType: %v", err)
	}
	_, err = GetMCPAppResourceFromReadResult("ui://a", ReadResourceResult{Contents: []ResourceContent{{URI: "ui://a", MimeType: MCPAppMimeType, Blob: "***"}}})
	if err == nil || err.Error() != "The string to be decoded is not correctly encoded." {
		t.Fatalf("bad blob: %v", err)
	}
}
