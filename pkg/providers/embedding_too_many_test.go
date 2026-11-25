package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
)

// TS embedding models throw TooManyEmbeddingValuesForCallError before any
// request when more values than maxEmbeddingsPerCall are passed.
func TestEmbeddingModelsReturnTooManyValuesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s", r.URL.Path)
	}))
	defer srv.Close()
	az, err := azure.New(azure.Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	bt := baseten.New(baseten.Config{APIKey: "k", ModelURL: srv.URL + "/sync/v1"})
	models := map[string]func() (provider.EmbeddingModel, error){
		"openai": func() (provider.EmbeddingModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("m")
		},
		"azure": func() (provider.EmbeddingModel, error) { return az.EmbeddingModel("m") },
		"cohere": func() (provider.EmbeddingModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("m")
		},
		"mistral": func() (provider.EmbeddingModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("m")
		},
		"together": func() (provider.EmbeddingModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("m")
		},
		"fireworks": func() (provider.EmbeddingModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("m")
		},
		"voyage": func() (provider.EmbeddingModel, error) {
			return voyage.New(voyage.Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("m")
		},
		"baseten": func() (provider.EmbeddingModel, error) { return bt.EmbeddingModel("m") },
	}
	for name, mk := range models {
		t.Run(name, func(t *testing.T) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			max := m.MaxEmbeddingsPerCall()
			_, err = m.DoEmbedMany(context.Background(), make([]string, max+1), nil)
			var tooMany *providererrors.TooManyEmbeddingValuesForCallError
			if !errors.As(err, &tooMany) || tooMany.MaxEmbeddingsPerCall != max || len(tooMany.Values) != max+1 {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
