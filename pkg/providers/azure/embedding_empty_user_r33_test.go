package azure

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// JSON.stringify keeps user: ” in TS, so an empty user option is forwarded.
func TestEmbeddingEmptyUserKeptR33(t *testing.T) {
	var body map[string]interface{}
	p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		return azureJSONResponse(200, `{"object":"list","data":[{"index":0,"embedding":[1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`, nil), nil
	})
	m := NewEmbeddingModel(p, "dep")
	opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"user": ""}}}
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, opts)
	if v, ok := body["user"]; !ok || v != "" {
		t.Fatalf("user = %v (present=%v), body = %#v", v, ok, body)
	}
}

// An unset user option is not sent (only an explicit "" is forwarded).
func TestEmbeddingUnsetUserOmittedR33(t *testing.T) {
	var body map[string]interface{}
	p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		return azureJSONResponse(200, `{"object":"list","data":[{"index":0,"embedding":[1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`, nil), nil
	})
	m := NewEmbeddingModel(p, "dep")
	opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{}}}
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, opts)
	if body == nil {
		t.Fatal("no request body captured")
	}
	if v, ok := body["user"]; ok {
		t.Fatalf("user = %v sent although the option is unset", v)
	}
}
