package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// r88 credential transport class (S2): the Entra token transport adds the
// token on the Azure endpoint and same-origin redirect hops, never on a hop
// to another origin (net/http drops Authorization there; the transport used
// to re-add it).
func TestR88integAzureTokenTransportSkipsCrossOriginHops(t *testing.T) {
	var mu sync.Mutex
	var foreignAuth, apiAuth, sameAuth []string
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		foreignAuth = append(foreignAuth, r.Header.Get("Authorization"))
		mu.Unlock()
	}))
	defer foreign.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.URL.Path == "/same" {
			sameAuth = append(sameAuth, r.Header.Get("Authorization"))
			mu.Unlock()
			return
		}
		apiAuth = append(apiAuth, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path == "/to-same" {
			http.Redirect(w, r, "/same", http.StatusTemporaryRedirect)
			return
		}
		http.Redirect(w, r, foreign.URL+"/x", http.StatusTemporaryRedirect)
	}))
	defer api.Close()
	client := azureHTTPClient(&http.Client{}, func(context.Context) (string, error) { return "entra-token", nil })
	for _, path := range []string{"/to-foreign", "/to-same"} {
		resp, err := client.Get(api.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(apiAuth) != 2 || apiAuth[0] != "Bearer entra-token" || apiAuth[1] != "Bearer entra-token" {
		t.Fatalf("API Authorization = %q", apiAuth)
	}
	if len(foreignAuth) != 1 || foreignAuth[0] != "" {
		t.Fatalf("cross-origin hop Authorization = %q, want none", foreignAuth)
	}
	if len(sameAuth) != 1 || sameAuth[0] != "Bearer entra-token" {
		t.Fatalf("same-origin hop Authorization = %q", sameAuth)
	}
}
