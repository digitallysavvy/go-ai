package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// JSON.stringify keeps user: ” in TS, so an empty user option is forwarded.
func TestEmbeddingEmptyUserKeptR33(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"}), "text-embedding-3-small")
	opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"user": ""}}}
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, opts)
	if v, ok := body["user"]; !ok || v != "" {
		t.Fatalf("user = %v (present=%v), body = %#v", v, ok, body)
	}
}

// An unset user option is not sent (only an explicit "" is forwarded).
func TestEmbeddingUnsetUserOmittedR33(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"}), "text-embedding-3-small")
	opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{}}}
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, opts)
	if body == nil {
		t.Fatal("no request body captured")
	}
	if v, ok := body["user"]; ok {
		t.Fatalf("user = %v sent although the option is unset", v)
	}
}
