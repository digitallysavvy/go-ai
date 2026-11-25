package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TS parseErrorResponse: z.string() accepts error:"", which is unmapped in
// OAUTH_ERRORS and becomes a ServerError carrying error_description.
func TestParseOAuthErrorResponseEmptyErrorIsServerError(t *testing.T) {
	err := ParseOAuthErrorResponse(jsonResponse(http.StatusBadRequest, `{"error":"","error_description":"boom"}`))
	if err.Code != OAuthErrorCodeServerError || err.Message != "boom" {
		t.Fatalf("err = %#v", err)
	}
	if oauthRefreshErrorEscalates(err) {
		t.Fatal("a ServerError must be swallowed by the refresh path")
	}
	// error_description: null fails .optional().
	bad := ParseOAuthErrorResponse(jsonResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":null}`))
	if !strings.Contains(bad.Message, "Invalid OAuth error response") {
		t.Fatalf("null description accepted: %#v", bad)
	}
}

// TS issuer is z.string(): an empty issuer passes the schema and fails the
// issuer match, not the schema.
func TestDiscoverAuthorizationServerMetadataEmptyIssuerFailsMatch(t *testing.T) {
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"issuer":"","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"]}`), nil
	})
	_, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "does not match expected issuer") {
		t.Fatalf("err = %v", err)
	}
	client = oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"]}`), nil
	})
	_, err = DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "missing required issuer") {
		t.Fatalf("absent issuer err = %v", err)
	}
}

// TS refreshAuthorization spreads the response over {refresh_token: old}: the
// old token survives only when the key is absent.
func TestRefreshOAuthAuthorizationEmptyRefreshTokenOverrides(t *testing.T) {
	client, _ := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"access_token":"NEW","token_type":"Bearer","refresh_token":""}`), nil
	})
	tokens, err := RefreshOAuthAuthorization(context.Background(), "https://auth.example.com", RefreshOAuthAuthorizationParams{
		Metadata:          &OAuthAuthorizationServerMetadata{TokenEndpoint: "https://auth.example.com/token"},
		ClientInformation: OAuthClientInformation{ClientID: "cid"},
		RefreshToken:      "old",
		HTTPClient:        client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokens.RefreshToken != "" {
		t.Fatalf("refresh token = %q, want empty override", tokens.RefreshToken)
	}
}
