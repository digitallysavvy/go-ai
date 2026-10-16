package mcp

import (
	"net/http"
	"testing"
)

// TS matches /(?:^|[,\s])scope="([^"]*)"/i: a name that merely ends in
// "scope" does not count, and the name is case-insensitive.
func TestExtractWWWAuthenticateParamsBoundaryAndCase(t *testing.T) {
	resp := func(h string) *http.Response {
		return &http.Response{Header: http.Header{"Www-Authenticate": []string{h}}}
	}
	got := ExtractWWWAuthenticateParams(resp(`Bearer mcp_scope="evil", Scope="a b", RESOURCE_METADATA="https://x.example/prm"`))
	if got.Scope != "a b" {
		t.Errorf("scope = %q", got.Scope)
	}
	if got.ResourceMetadataURL == nil || got.ResourceMetadataURL.String() != "https://x.example/prm" {
		t.Errorf("resource metadata = %v", got.ResourceMetadataURL)
	}
	got = ExtractWWWAuthenticateParams(resp(`Bearer my_resource_metadata="https://evil.example/prm", error="x"`))
	if got.ResourceMetadataURL != nil || got.Scope != "" {
		t.Errorf("suffix match accepted: %+v", got)
	}
}

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
		prm := OAuthProtectedResourceMetadata{Resource: "https://r.example", AuthorizationServers: []string{bad}}
		if err := validateOAuthProtectedResourceMetadata(prm); err == nil {
			t.Errorf("authorization server %q accepted", bad)
		}
	}
}
