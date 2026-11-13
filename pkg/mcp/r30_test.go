package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// TS getMCPAppToolMeta throws `Invalid MCP App resource URI: ${JSON.stringify(uri)}`;
// JSON.stringify leaves <, > and & unescaped.
func TestR30InvalidAppResourceURIMessageNotHTMLEscaped(t *testing.T) {
	_, err := GetMCPAppToolMeta(MCPTool{Name: "t", Meta: map[string]interface{}{
		"ui": map[string]interface{}{"resourceUri": "http://x/?a=1&b=<2>"},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
	want := `Invalid MCP App resource URI: "http://x/?a=1&b=<2>"`
	if err.Error() != want {
		t.Fatalf("got %q want %q", err.Error(), want)
	}
}

// TS builds the protected-resource metadata URL from the WHATWG pathname,
// which keeps percent-encoding.
func TestR30ProtectedResourceMetadataURLKeepsEncodedPath(t *testing.T) {
	got, err := protectedResourceMetadataURL("https://h.example/a%2Fb/?x=1", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://h.example/.well-known/oauth-protected-resource/a%2Fb?x=1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

type saveTokensFailsProvider struct{ *fakeOAuthProvider }

func (p saveTokensFailsProvider) SaveTokens(ctx context.Context, tokens OAuthTokens) error {
	return errors.New("disk full")
}

// TS auth() saves refreshed tokens inside the refresh try block: a plain
// (non-OAuth) error there is swallowed and a fresh authorization starts.
func TestR30AuthSaveTokensFailureAfterRefreshFallsThroughToRedirect(t *testing.T) {
	base := newFakeOAuthProvider()
	base.clientInfo = &OAuthClientInformation{ClientID: "existing-client"}
	base.tokens = &OAuthTokens{
		AccessToken:         "OLD_AT",
		RefreshToken:        "RT",
		Issuer:              "https://auth.example.com",
		AuthorizationServer: "https://auth.example.com/",
		TokenEndpoint:       "https://auth.example.com/token",
	}
	client := oauthFlowTestServer(t, map[string]func(*http.Request) (*http.Response, error){
		"/.well-known/oauth-protected-resource": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://auth.example.com/mcp","authorization_servers":["https://auth.example.com"]}`), nil
		},
		"/.well-known/oauth-authorization-server": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, oauthFlowASMetadataJSON), nil
		},
		"/token": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"access_token":"NEW_AT","token_type":"Bearer"}`), nil
		},
	})
	result, err := Auth(context.Background(), saveTokensFailsProvider{base}, AuthOptions{
		ServerURL:  "https://auth.example.com/mcp",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("Auth error: %v", err)
	}
	if result != AuthResultRedirect {
		t.Fatalf("result = %s, want REDIRECT", result)
	}
}
