package together

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS OpenAICompatibleEmbeddingModel returns the deprecated 'openai-compatible'
// providerOptions warning and passes response.providerMetadata through.
func TestEmbeddingReturnsWarningsAndProviderMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1]}],"usage":{"prompt_tokens":1},"providerMetadata":{"x":{"a":1}}}`))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "m")
	opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"openai-compatible": map[string]interface{}{"user": "u"}}}
	many, err := m.DoEmbedMany(context.Background(), []string{"a"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(many.Warnings) != 1 || many.Warnings[0].Type != "deprecated" {
		t.Fatalf("warnings = %+v", many.Warnings)
	}
	if many.ProviderMetadata["x"] == nil {
		t.Fatalf("providerMetadata = %+v", many.ProviderMetadata)
	}
	one, err := m.DoEmbed(context.Background(), "a", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Warnings) != 1 || one.ProviderMetadata["x"] == nil {
		t.Fatalf("single result = %+v", one)
	}
}
