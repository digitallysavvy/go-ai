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
