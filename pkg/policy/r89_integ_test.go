package policy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// r89integRedirectPair returns an origin server that redirects every request
// (307, method and body kept) to another port of the same host, and a
// function reporting the Authorization the second origin saw. Fetch drops
// Authorization on any cross-origin hop (scheme, host or port); net/http
// alone keeps it when only the port changes.
func r89integRedirectPair(t *testing.T, body string) (string, func() (string, bool)) {
	t.Helper()
	var mu sync.Mutex
	var got string
	var hit bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got, hit = r.Header.Get("Authorization"), true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(other.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	return origin.URL, func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		return got, hit
	}
}

func TestR89integPolicyRedirectDropsAuthorization(t *testing.T) {
	url, seen := r89integRedirectPair(t, `{"result":true}`)
	c := HTTPPolicyClient(url, HTTPClientOptions{Headers: map[string]string{"Authorization": "Bearer opa"}})
	_, _ = c.Evaluate(context.Background(), "authz/allow", map[string]any{})
	got, hit := seen()
	if !hit {
		t.Fatal("redirect target not reached")
	}
	if got != "" {
		t.Fatalf("other origin got Authorization %q", got)
	}
}
