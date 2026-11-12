package perplexity

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

// doGenerate is TS postJsonToApi (`!response.ok` runs the failed-response
// handler): an unfollowed 3xx is the Perplexity API error, not a response.
func TestR94provgGenerateNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"id":"r","created_at":1,"model":"m","object":"response","status":"completed","output":[]}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "sonar")
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 300 {
		t.Fatalf("err = %T %v, want the 300 API error", err, err)
	}
}
