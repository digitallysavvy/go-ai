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
