package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestR88provgAuthTransportRedirectHopGetsNoCredentials covers the
// credential transport class sweep (S2): the gateway transport runs once per
// redirect hop, so a redirect to another origin must not receive the bearer
// token or the auth-method header, and the resolver is not called for it.
func TestR88provgAuthTransportRedirectHopGetsNoCredentials(t *testing.T) {
	var mu sync.Mutex
	var foreignAuth, foreignMethod string
	foreignHits := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		foreignHits++
		foreignAuth = r.Header.Get("Authorization")
		foreignMethod = r.Header.Get("ai-gateway-auth-method")
		mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	}))
	defer foreign.Close()
	var apiAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		apiAuth = r.Header.Get("Authorization")
		mu.Unlock()
		http.Redirect(w, r, foreign.URL+"/x", http.StatusTemporaryRedirect)
	}))
	defer api.Close()

	resolverCalls := 0
	client := newGatewayHTTPClient(nil, func(context.Context) (string, string, error) {
		resolverCalls++
		return "gw-key", "api-key", nil
	})
	resp, err := client.Get(api.URL + "/v4/ai/config")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if apiAuth != "Bearer gw-key" {
		t.Fatalf("API hop Authorization = %q", apiAuth)
	}
	if foreignHits != 1 || foreignAuth != "" || foreignMethod != "" {
		t.Fatalf("foreign hop: hits=%d Authorization=%q auth-method=%q, want 1 hit and no credentials", foreignHits, foreignAuth, foreignMethod)
	}
	if resolverCalls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolverCalls)
	}
}
