package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type r92toolsRoundTripper func(*http.Request) (*http.Response, error)

func (f r92toolsRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func r92toolsASMetadata(issuer string) string {
	return `{"issuer":"` + issuer + `","authorization_endpoint":"` + issuer + `/authorize","token_endpoint":"` + issuer + `/token","response_types_supported":["code"],"code_challenge_methods_supported":["S256"]}`
}

// TS 88b95757c1 oauth-discovery-runtime.test.ts: the configured resource
// origin (164) and a configured authorization-server origin (173) are
// fetched although they are loopback; a same-origin discovery redirect is
// followed (205); a configured origin's redirect to another, private host
// is blocked after one request (182); an explicitly injected transport is
// used for untrusted discovery (221). The TS-only "requires a custom fetch
// with connect-time DNS validation" error is the known difference "MCP
// OAuth: Go always dials untrusted OAuth hops through the DNS-pinned
// transport".
func TestR92toolsOAuthDiscoveryTrustedOrigins(t *testing.T) {
	var srvURL string
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource"):
			_, _ = io.WriteString(w, `{"resource":"`+srvURL+`","authorization_servers":["`+srvURL+`"]}`)
		case r.URL.Path == "/.well-known/oauth-authorization-server/redirected":
			http.Redirect(w, r, srvURL+"/metadata", http.StatusFound)
		case r.URL.Path == "/metadata":
			_, _ = io.WriteString(w, r92toolsASMetadata(srvURL+"/redirected"))
		case r.URL.Path == "/.well-known/oauth-authorization-server/private":
			http.Redirect(w, r, "http://10.0.0.1/metadata", http.StatusFound)
		case r.URL.Path == "/.well-known/oauth-authorization-server":
			_, _ = io.WriteString(w, r92toolsASMetadata(srvURL))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	ctx := context.Background()

	prm, err := DiscoverOAuthProtectedResourceMetadata(ctx, srvURL, OAuthDiscoveryOptions{})
	if err != nil || prm.Resource != srvURL {
		t.Fatalf("resource origin (164): %+v, %v", prm, err)
	}
	as, err := DiscoverAuthorizationServerMetadata(ctx, srvURL, OAuthDiscoveryOptions{TrustedOrigin: srvURL})
	if err != nil || as.Issuer != srvURL {
		t.Fatalf("configured AS origin (173): %+v, %v", as, err)
	}
	as, err = DiscoverAuthorizationServerMetadata(ctx, srvURL+"/redirected", OAuthDiscoveryOptions{TrustedOrigin: srvURL})
	if err != nil || as.Issuer != srvURL+"/redirected" {
		t.Fatalf("same-origin redirect (205): %+v, %v", as, err)
	}
	before := requests.Load()
	if _, err := DiscoverAuthorizationServerMetadata(ctx, srvURL+"/private", OAuthDiscoveryOptions{TrustedOrigin: srvURL}); err == nil {
		t.Fatal("untrusted redirect from a configured origin (182) followed")
	}
	if n := requests.Load() - before; n != 1 {
		t.Fatalf("untrusted redirect (182): %d requests, want 1", n)
	}

	const issuer = "https://auth.example.com"
	var custom atomic.Int32
	client := &http.Client{Transport: r92toolsRoundTripper(func(r *http.Request) (*http.Response, error) {
		custom.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(r92toolsASMetadata(issuer))), Request: r}, nil
	})}
	as, err = DiscoverAuthorizationServerMetadata(ctx, issuer, OAuthDiscoveryOptions{HTTPClient: client})
	if err != nil || as.Issuer != issuer || custom.Load() != 1 {
		t.Fatalf("injected transport (221): %+v, %v, %d calls", as, err, custom.Load())
	}
}
