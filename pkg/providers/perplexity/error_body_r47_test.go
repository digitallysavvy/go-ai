package perplexity

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS createJsonErrorResponseHandler falls back to the status text (status code
// and headers kept) when the body does not match perplexityErrorSchema, e.g. an
// explicit null `error`; a matching body keeps the resolved message.
func TestR47_ErrorBodyNotMatchingSchemaFallsBackToStatusText(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":null}`:             "Bad Gateway",
		`{"error":5}`:                "Bad Gateway",
		`{"detail":[{"x":1}]}`:       "Bad Gateway",
		`[1]`:                        "Bad Gateway",
		`{"error":{"message":"no"}}`: "no",
	} {
		_, model := newPerplexityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Req", "7")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(body))
		})
		_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("%s: err = %#v", body, err)
		}
		if pe.Message != want || pe.StatusCode != 502 || pe.ResponseBody != body || pe.ResponseHeaders["x-req"] != "7" {
			t.Fatalf("%s: %+v", body, pe)
		}
	}
}

// The embedding model uses the same createJsonErrorResponseHandler as the
// language model: the status code and the schema message survive.
func TestR47_EmbeddingErrorBodyResolvesMessageAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":[{"msg":"slow"},{"msg":"down"}]}`))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{BaseURL: srv.URL, APIKey: "k"}), "pplx-embed-v1-4b")
	_, err := m.DoEmbedMany(t.Context(), []string{"a"}, nil)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 429 || pe.Message != "slow, down" {
		t.Fatalf("err = %#v", err)
	}
}
