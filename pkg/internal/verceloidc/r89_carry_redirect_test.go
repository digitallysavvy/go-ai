// Target: pkg/internal/verceloidc/r89_carry_redirect_test.go (integrator)
package verceloidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A redirect to another port of the same host must not carry the Bearer
// token (fetch drops Authorization on any cross-origin hop).
func TestR89carryRefreshRedirectDropsAuthorization(t *testing.T) {
	var got string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"token":"x"}`))
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	r := New(Options{APIBaseURL: origin.URL})
	_, _ = r.requestOIDCToken(context.Background(), "auth-token", "prj", "")
	if got != "" {
		t.Fatalf("other origin got Authorization %q", got)
	}
}
