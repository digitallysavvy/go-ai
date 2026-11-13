package baseten

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS basetenErrorSchema: `error` is a bare string (Model APIs) or an
// OpenAI-shaped object (dedicated deployments); errorToMessage reads either.
func TestR89provcErrorEnvelopes(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`{"error":"please check the model you provided"}`, "please check the model you provided"},
		{`{"error":{"message":"model not found","type":"invalid_request_error","code":404}}`, "model not found"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(c.body))
		}))
		lm, err := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		_, err = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		server.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != c.want {
			t.Fatalf("body %s: err = %v", c.body, err)
		}
	}
}

func TestR89provcEmbeddingProviderName(t *testing.T) {
	em, err := New(Config{APIKey: "k", ModelURL: "https://model-x.api.baseten.co/environments/production/sync"}).EmbeddingModel("e")
	if err != nil {
		t.Fatal(err)
	}
	if em.Provider() != "baseten.embedding" {
		t.Fatalf("provider = %q", em.Provider())
	}
}
