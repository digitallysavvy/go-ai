package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// z.string().url() accepts anything `new URL()` parses, including the
// host-less custom scheme RFC 8252 native apps register as a redirect URI.
func TestOAuthURLValidationAcceptsHostlessCustomSchemes(t *testing.T) {
	for _, ok := range []string{
		"com.example.app:/oauth2redirect",
		"myapp://callback",
		"mailto:a@example.com",
		"urn:ietf:wg:oauth:2.0:oob",
		"  https://example.com/cb  ",
		"http:example.com",
	} {
		if !isSafeOAuthURL(ok) {
			t.Errorf("isSafeOAuthURL(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{
		"", "example.com/cb", "/relative", "http://", "https:/", "http:",
		"javascript:alert(1)", "data:text/html,x", "vbscript:x", "JavaScript:alert(1)",
	} {
		if isSafeOAuthURL(bad) {
			t.Errorf("isSafeOAuthURL(%q) = true, want false", bad)
		}
	}

	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"client_id":"c","redirect_uris":["com.example.app:/oauth2redirect"]}`), nil
	})
	full, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", nil,
		OAuthClientMetadata{RedirectURIs: []string{"com.example.app:/oauth2redirect"}}, client)
	if err != nil {
		t.Fatalf("registration with a native redirect URI failed: %v", err)
	}
	if len(full.RedirectURIs) != 1 {
		t.Fatalf("redirect_uris = %v", full.RedirectURIs)
	}
}

// A float expires_in / issued-at is a valid z.number().
func TestOAuthResponsesAcceptFractionalNumbers(t *testing.T) {
	tokens, err := decodeOAuthTokenResponse(jsonResponse(http.StatusOK, `{"access_token":"a","token_type":"Bearer","expires_in":3599.5}`))
	if err != nil {
		t.Fatalf("float expires_in rejected: %v", err)
	}
	if tokens.ExpiresIn == nil || *tokens.ExpiresIn != 3599 {
		t.Fatalf("expires_in = %v, want 3599", tokens.ExpiresIn)
	}
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"client_id":"c","redirect_uris":["https://app/cb"],"client_id_issued_at":1700000000.0,"client_secret_expires_at":0.0}`), nil
	})
	full, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", nil, OAuthClientMetadata{RedirectURIs: []string{"https://app/cb"}}, client)
	if err != nil {
		t.Fatalf("float client_id_issued_at rejected: %v", err)
	}
	if full.ClientIDIssuedAt == nil || *full.ClientIDIssuedAt != 1700000000 {
		t.Fatalf("client_id_issued_at = %v", full.ClientIDIssuedAt)
	}
}

// Required arrays are z.array(z.string()) too: a null element fails.
func TestOAuthMetadataRequiredArraysRejectNonStrings(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"response_types null element", `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/a","token_endpoint":"https://auth.example.com/t","response_types_supported":[null]}`},
		{"response_types number", `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/a","token_endpoint":"https://auth.example.com/t","response_types_supported":[1]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, tc.body), nil
			})
			if _, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client}); err == nil {
				t.Fatal("metadata accepted")
			}
		})
	}
	oidc := `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/a","token_endpoint":"https://auth.example.com/t","jwks_uri":"https://auth.example.com/j","response_types_supported":["code"],"code_challenge_methods_supported":["S256"],`
	for _, extra := range []string{
		`"subject_types_supported":[null],"id_token_signing_alg_values_supported":["RS256"]}`,
		`"subject_types_supported":["public"],"id_token_signing_alg_values_supported":[null]}`,
	} {
		client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/.well-known/openid-configuration" {
				return jsonResponse(http.StatusOK, oidc+extra), nil
			}
			return jsonResponse(http.StatusNotFound, `{}`), nil
		})
		if _, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client}); err == nil {
			t.Errorf("OIDC metadata with %s accepted", extra)
		}
	}
}

// application_type is compared after JSON decoding ("native" is "native").
func TestOAuthApplicationTypeDecodesEscapes(t *testing.T) {
	if !oauthFieldMatches(json.RawMessage(`"native"`), oauthFieldApplicationType) {
		t.Fatal(`"native" rejected`)
	}
	if oauthFieldMatches(json.RawMessage(`"desktop"`), oauthFieldApplicationType) {
		t.Fatal(`"desktop" accepted`)
	}
}

// OAuthClientMetadata carries the inline jwks (TS `jwks: z.any()`) in the
// registration request and response.
func TestOAuthClientMetadataJWKSRoundTrip(t *testing.T) {
	var sent map[string]interface{}
	client, captured := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		sent = req.json
		return jsonResponse(http.StatusOK, `{"client_id":"c","redirect_uris":["https://app/cb"],"jwks":{"keys":[{"kty":"RSA"}]}}`), nil
	})
	_ = captured
	full, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", nil, OAuthClientMetadata{
		RedirectURIs: []string{"https://app/cb"},
		JWKS:         map[string]interface{}{"keys": []interface{}{map[string]interface{}{"kty": "RSA"}}},
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	if sent["jwks"] == nil {
		t.Errorf("registration request dropped jwks: %v", sent)
	}
	if full.JWKS == nil {
		t.Errorf("registration response dropped jwks")
	}
}

// The WWW-Authenticate resource_metadata URL is any parseable URL (a hostless
// one fails the same-origin check instead of being silently ignored).
func TestExtractWWWAuthenticateParamsKeepsHostlessResourceMetadataURL(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Www-Authenticate": []string{`Bearer resource_metadata="urn:x:y"`}}}
	params := ExtractWWWAuthenticateParams(resp)
	if params.ResourceMetadataURL == nil {
		t.Fatal("hostless resource_metadata URL dropped")
	}
	if err := AssertOAuthResourceMetadataURLSameOrigin("https://mcp.example.com", params.ResourceMetadataURL.String()); err == nil {
		t.Fatal("hostless resource_metadata URL passed the same-origin check")
	}
}

func TestSplitMCPAppToolsReturnsEmptyArraysNotNull(t *testing.T) {
	model, app, err := SplitMCPAppTools(ListToolsResult{Tools: []MCPTool{{Name: "plain"}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, res := range map[string]ListToolsResult{"modelVisible": model, "appVisible": app} {
		data, _ := json.Marshal(res)
		var out map[string]interface{}
		_ = json.Unmarshal(data, &out)
		if _, ok := out["tools"].([]interface{}); !ok {
			t.Errorf("%s tools = %v, want an array", name, out["tools"])
		}
	}
}
