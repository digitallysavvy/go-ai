package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
)

// TS loadApiKey falls back to the provider's environment variable when no key
// is configured.
func TestAPIKeyFallsBackToEnvironment(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cases := []struct {
		name, env string
		model     func() provider.LanguageModel
	}{
		{"groq", "GROQ_API_KEY", func() provider.LanguageModel {
			m, _ := groq.New(groq.Config{BaseURL: srv.URL}).LanguageModel("m")
			return m
		}},
		{"cohere", "COHERE_API_KEY", func() provider.LanguageModel {
			m, _ := cohere.New(cohere.Config{BaseURL: srv.URL}).LanguageModel("m")
			return m
		}},
		{"mistral", "MISTRAL_API_KEY", func() provider.LanguageModel {
			m, _ := mistral.New(mistral.Config{BaseURL: srv.URL}).LanguageModel("m")
			return m
		}},
		{"deepseek", "DEEPSEEK_API_KEY", func() provider.LanguageModel {
			m, _ := deepseek.New(deepseek.Config{BaseURL: srv.URL}).LanguageModel("m")
			return m
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(c.env, "env-key")
			got = ""
			_, _ = c.model().DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if got != "Bearer env-key" {
				t.Fatalf("Authorization = %q, want Bearer env-key", got)
			}
		})
	}
}
