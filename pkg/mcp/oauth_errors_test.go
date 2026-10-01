package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// oversizedOAuthBody returns a string of n bytes, comfortably over
// maxOAuthResponseBytes, for the R2-5 regression tests below.
func oversizedOAuthBody(n int) string {
	var b strings.Builder
	b.Grow(n)
	chunk := strings.Repeat("a", 4096)
	for b.Len()+len(chunk) <= n {
		b.WriteString(chunk)
	}
	for b.Len() < n {
		b.WriteByte('a')
	}
	return b.String()
}

// TestOAuthTokenResponseReadIsCapped is a permanent regression test for
// R2-5: decodeOAuthTokenResponse used json.NewDecoder(resp.Body).Decode with
// no size cap, so a malicious/compromised OAuth authorization server could
// exhaust memory returning an oversized response during token exchange.
// Adapted from the bug report's TestZZBugReviewOAuthTokenResponseUnboundedRead
// (state/parity/sep_23_2026/bug-review/R2.md). Unlike that test (which
// asserted the *vulnerable* behavior -- the oversized field was fully
// buffered), this asserts the fixed behavior: the read is capped and errors
// out cleanly instead of buffering it all.
func TestOAuthTokenResponseReadIsCapped(t *testing.T) {
	const oversized = 2 << 20 // 2 MiB, well over the 1 MiB cap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"`))
		_, _ = w.Write([]byte(oversizedOAuthBody(oversized)))
		_, _ = w.Write([]byte(`","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	tokens, err := decodeOAuthTokenResponse(resp)
	if err == nil {
		t.Fatalf("expected decodeOAuthTokenResponse to reject an oversized response, got tokens=%#v", tokens)
	}
}

// TestOAuthErrorResponseReadIsCapped covers the same R2-5 class of bug for
// ParseOAuthErrorResponse (oauth_errors.go), which used an unbounded
// io.ReadAll(resp.Body).
func TestOAuthErrorResponseReadIsCapped(t *testing.T) {
	const oversized = 2 << 20
	resp := jsonResponse(http.StatusInternalServerError, oversizedOAuthBody(oversized))

	oauthErr := ParseOAuthErrorResponse(resp)
	if oauthErr.Code != OAuthErrorCodeServerError {
		t.Fatalf("Code = %q, want %q", oauthErr.Code, OAuthErrorCodeServerError)
	}
	if len(oauthErr.Message) >= oversized {
		t.Fatalf("Message length = %d, expected the oversized body to never be fully buffered into it", len(oauthErr.Message))
	}
}

// TestRegisterOAuthClientResponseReadIsCapped covers RegisterOAuthClient's
// dynamic client registration response decode (oauth_flow.go), the third
// R2-5 call site.
func TestRegisterOAuthClientResponseReadIsCapped(t *testing.T) {
	const oversized = 2 << 20
	client, _ := newOAuthCapturingClient(t, func(req *oauthCapturedRequest) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, `{"client_id":"`+oversizedOAuthBody(oversized)+`"}`), nil
	})

	_, err := RegisterOAuthClient(context.Background(), "https://auth.example.com", &OAuthAuthorizationServerMetadata{RegistrationEndpoint: "https://auth.example.com/register"}, OAuthClientMetadata{
		RedirectURIs: []string{"https://app.example.com/cb"},
	}, client)
	if err == nil {
		t.Fatal("expected RegisterOAuthClient to reject an oversized registration response")
	}
}

// TestOAuthProtectedResourceMetadataReadIsCapped and
// TestAuthorizationServerMetadataReadIsCapped cover oauth.go's two metadata
// GET decode sites, the remaining R2-5 call sites.
func TestOAuthProtectedResourceMetadataReadIsCapped(t *testing.T) {
	const oversized = 2 << 20
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"resource":"`+oversizedOAuthBody(oversized)+`","authorization_servers":["https://auth.example.com"]}`), nil
	})

	_, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), "https://resource.example.com/mcp", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil {
		t.Fatal("expected DiscoverOAuthProtectedResourceMetadata to reject an oversized metadata response")
	}
}

func TestAuthorizationServerMetadataReadIsCapped(t *testing.T) {
	const oversized = 2 << 20
	client := oauthTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"issuer":"`+oversizedOAuthBody(oversized)+`"}`), nil
	})

	_, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://auth.example.com", OAuthDiscoveryOptions{HTTPClient: client})
	if err == nil {
		t.Fatal("expected DiscoverAuthorizationServerMetadata to reject an oversized metadata response")
	}
}
