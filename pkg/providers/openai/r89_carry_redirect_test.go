// Target: pkg/providers/openai/r89_carry_redirect_test.go (provb)
package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A redirect to another port of the same host must not carry the Bearer key
// (fetch drops Authorization on any cross-origin hop).
func TestR89carryModelCatalogRedirectDropsAuthorization(t *testing.T) {
	var got string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-x"}]}`))
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/models", http.StatusFound)
	}))
	defer origin.Close()
	if _, err := (ModelCatalogRefresher{APIKey: "k", Endpoint: origin.URL + "/v1/models"}).Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("other origin got Authorization %q", got)
	}
}
