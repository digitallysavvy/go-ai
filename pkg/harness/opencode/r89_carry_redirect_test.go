// Target: pkg/harness/opencode/r89_carry_redirect_test.go (tools)
package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A redirect to another port of the same host must not carry the Bearer
// access token (fetch drops Authorization on any cross-origin hop).
func TestR89carryGitLabDirectAccessRedirectDropsAuthorization(t *testing.T) {
	var got string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	_, _ = requestOpenCodeGitLabDirectAccess(context.Background(), nil, "access-token", origin.URL, "https://gateway.example")
	if got != "" {
		t.Fatalf("other origin got Authorization %q", got)
	}
}
