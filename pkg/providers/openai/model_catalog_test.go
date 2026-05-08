package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelCatalogRefresherFetch(t *testing.T) {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.5"},{"id":"gpt-5.5-chat-latest"}]}`))
	}))
	defer server.Close()

	refresher := ModelCatalogRefresher{
		APIKey:     "test-key",
		HTTPClient: server.Client(),
		Endpoint:   server.URL,
	}
	models, err := refresher.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want Bearer test-key", auth)
	}
	if len(models) != 2 || models[0].ID != "gpt-5.5" || models[1].ID != "gpt-5.5-chat-latest" {
		t.Fatalf("unexpected models: %#v", models)
	}
}
