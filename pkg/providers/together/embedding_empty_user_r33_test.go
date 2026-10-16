package together

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
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "embed")
	opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"togetherai": map[string]interface{}{"user": ""}}}
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, opts)
	if v, ok := body["user"]; !ok || v != "" {
		t.Fatalf("user = %v (present=%v), body = %#v", v, ok, body)
	}
}
