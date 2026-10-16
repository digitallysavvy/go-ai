package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
)

// A 2xx embedding response with no embeddings must not panic DoEmbed (it
// indexed [0] unchecked); it returns an error instead.
func TestDoEmbedEmptyEmbeddingsDoesNotPanic(t *testing.T) {
	const compat = `{"object":"list","data":[],"model":"m","usage":{"prompt_tokens":1,"total_tokens":1}}`
	cases := map[string]struct {
		body string
		mk   func(url string) (provider.EmbeddingModel, error)
	}{
		"cohere": {`{"id":"x","embeddings":{"float":[]},"texts":[],"meta":{"billed_units":{"input_tokens":1}}}`, func(u string) (provider.EmbeddingModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: u}).EmbeddingModel("m")
		}},
		"fireworks": {compat, func(u string) (provider.EmbeddingModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: u}).EmbeddingModel("m")
		}},
		"mistral": {compat, func(u string) (provider.EmbeddingModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: u}).EmbeddingModel("m")
		}},
		"ollama": {compat, func(u string) (provider.EmbeddingModel, error) {
			return ollama.New(ollama.Config{BaseURL: u}).EmbeddingModel("m")
		}},
		"together": {compat, func(u string) (provider.EmbeddingModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: u}).EmbeddingModel("m")
		}},
		"voyage": {`{"object":"list","data":[],"model":"m","usage":{"total_tokens":1}}`, func(u string) (provider.EmbeddingModel, error) {
			return voyage.New(voyage.Config{APIKey: "k", BaseURL: u}).EmbeddingModel("m")
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			m, err := tc.mk(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("DoEmbed panicked: %v", r)
				}
			}()
			if _, err := m.DoEmbed(context.Background(), "hi", nil); err == nil {
				t.Fatal("DoEmbed with no embeddings returned no error")
			}
		})
	}
}
