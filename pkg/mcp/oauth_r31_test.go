package mcp

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

// TS shouldAttemptFallback is `!response || (4xx && pathname !== '/')`: after
// a transport failure the root well-known URL is requested again even when the
// server URL has no path, so the failing request is made four times (with and
// without MCP-Protocol-Version, for both attempts).
func TestDiscoverOAuthProtectedResourceMetadataRetriesRootAfterTransportFailure(t *testing.T) {
	var calls atomic.Int32
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("connection refused")
	})
	_, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://mcp.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil {
		t.Fatal("want the not-implemented error")
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("requests = %d, want 4", got)
	}
}

// TS `const [type, scheme] = header.split(' ')` with `!scheme` rejects a
// challenge whose second space-separated token is empty.
func TestExtractWWWAuthenticateParamsRequiresSecondToken(t *testing.T) {
	for header, wantScope := range map[string]string{
		`Bearer  scope="a"`:          "",
		`Bearer scope="a"`:           "a",
		`Bearer `:                    "",
		`Bearer`:                     "",
		`bearer realm="x" scope="b"`: "b",
	} {
		resp := &http.Response{Header: http.Header{"Www-Authenticate": []string{header}}}
		if got := ExtractWWWAuthenticateParams(resp).Scope; got != wantScope {
			t.Errorf("%q: scope = %q, want %q", header, got, wantScope)
		}
	}
}

// TS normalizeUrl is `new URL(x).href`: "/" for an empty path (even before a
// query), lower-case host, default port dropped.
func TestOAuthURLHrefMatchesWHATWG(t *testing.T) {
	for in, want := range map[string]string{
		"https://auth.example.com":          "https://auth.example.com/",
		"https://auth.example.com?x=1":      "https://auth.example.com/?x=1",
		"https://AUTH.Example.com:443/p":    "https://auth.example.com/p",
		"http://auth.example.com:80":        "http://auth.example.com/",
		"https://auth.example.com:8443/p?q": "https://auth.example.com:8443/p?q",
		"http://[::1]:3000":                 "http://[::1]:3000/",
	} {
		if got := normalizeOAuthURL(in); got != want {
			t.Errorf("normalizeOAuthURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStartOAuthAuthorizationNormalizesEmptyPath(t *testing.T) {
	got, _, err := StartOAuthAuthorization("https://auth.example.com", StartOAuthAuthorizationParams{
		Metadata: &OAuthAuthorizationServerMetadata{
			AuthorizationEndpoint:         "https://auth.example.com",
			ResponseTypesSupported:        []string{"code"},
			CodeChallengeMethodsSupported: []string{"S256"},
		},
		ClientInformation: OAuthClientInformation{ClientID: "c"},
		RedirectURL:       "https://app/cb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://auth.example.com/?response_type=code"; got[:len(want)] != want {
		t.Fatalf("authorization URL = %s, want prefix %s", got, want)
	}
}

func TestRegisterOAuthClientSendsOnlyContentType(t *testing.T) {
	var accept string
	var sawAccept bool
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		_, sawAccept = req.Header["Accept"]
		accept = req.Header.Get("Accept")
		return jsonResponse(http.StatusOK, `{"client_id":"c","redirect_uris":["https://app/cb"]}`), nil
	})
	_, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", nil, OAuthClientMetadata{RedirectURIs: []string{"https://app/cb"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	if sawAccept {
		t.Fatalf("registration sent Accept %q; TS sends Content-Type only", accept)
	}
}

// Zod validates every declared field of the PRM / AS metadata schemas; a
// malformed optional field (or a JSON null, which .optional() rejects) fails
// the whole parse.
func TestOAuthMetadataSchemasRejectMalformedOptionalFields(t *testing.T) {
	prm := func(extra string) error {
		client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"resource":"https://mcp.example.com",`+extra+`}`), nil
		})
		_, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://mcp.example.com", OAuthDiscoveryOptions{HTTPClient: client})
		return err
	}
	if err := prm(`"authorization_servers":["https://a.example.com"],"resource_name":"x"`); err != nil {
		t.Fatalf("valid PRM: %v", err)
	}
	for _, bad := range []string{
		`"jwks_uri":"not a url"`,
		`"resource_policy_uri":5`,
		`"resource_name":null`,
		`"scopes_supported":null`,
		`"bearer_methods_supported":[1]`,
		`"dpop_bound_access_tokens_required":"yes"`,
		`"authorization_servers":null`,
	} {
		if err := prm(bad); err == nil {
			t.Errorf("PRM with %s was accepted", bad)
		}
	}

	as := func(candidate, extra string) error {
		client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/a","token_endpoint":"https://auth.example.com/t","response_types_supported":["code"],"jwks_uri":"https://auth.example.com/j","subject_types_supported":["public"],"id_token_signing_alg_values_supported":["RS256"],"code_challenge_methods_supported":["S256"],`+extra+`}`), nil
		})
		_, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
		return err
	}
	if err := as("", `"scopes_supported":["a"]`); err != nil {
		t.Fatalf("valid AS: %v", err)
	}
	for _, bad := range []string{
		`"scopes_supported":null`,
		`"grant_types_supported":"authorization_code"`,
		`"token_endpoint_auth_signing_alg_values_supported":[1]`,
		`"authorization_response_iss_parameter_supported":null`,
	} {
		if err := as("", bad); err == nil {
			t.Errorf("AS metadata with %s was accepted", bad)
		}
	}
}

// OAuthTokensSchema / OAuthClientInformationFullSchema reject a null or
// wrongly typed optional field.
func TestOAuthTokenAndRegistrationResponsesRejectNullOptionalFields(t *testing.T) {
	for _, bad := range []string{`"refresh_token":null`, `"expires_in":null`, `"scope":5`} {
		resp := jsonResponse(http.StatusOK, `{"access_token":"a","token_type":"Bearer",`+bad+`}`)
		if _, err := decodeOAuthTokenResponse(resp); err == nil {
			t.Errorf("token response with %s was accepted", bad)
		}
	}
	if _, err := decodeOAuthTokenResponse(jsonResponse(http.StatusOK, `{"access_token":"a","token_type":"Bearer","expires_in":3600}`)); err != nil {
		t.Fatalf("valid token response: %v", err)
	}
	for _, bad := range []string{`"client_secret":null`, `"application_type":"desktop"`, `"client_id_issued_at":"1"`, `"contacts":[1]`} {
		client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"client_id":"c","redirect_uris":["https://app/cb"],`+bad+`}`), nil
		})
		if _, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", nil, OAuthClientMetadata{RedirectURIs: []string{"https://app/cb"}}, client); err == nil {
			t.Errorf("registration response with %s was accepted", bad)
		}
	}
}
