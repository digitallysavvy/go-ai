package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// The openai-compatible wrappers (baseten, deepinfra) read dimensions/user
// from providerOptions.openaiCompatible and their own provider namespace, not
// providerOptions.openai (TS OpenAICompatibleEmbeddingModel).
func TestEmbeddingCompatibleProviderOptions(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1", Name: "deepinfra", OpenAICompatible: true}), "e")
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		Dimensions: dimPtr(256),
		ProviderOptions: map[string]interface{}{
			"openaiCompatible": map[string]interface{}{"user": "u1", "dimensions": float64(64)},
			"deepinfra":        map[string]interface{}{"dimensions": float64(512)},
			"openai":           map[string]interface{}{"dimensions": float64(999), "user": "ignored"},
		},
	})
	if body["dimensions"] != float64(512) || body["user"] != "u1" {
		t.Fatalf("body = %#v", body)
	}
}
