package vercel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Vercel reads VERCEL_API_KEY and never sends OPENAI_API_KEY to Vercel.
func TestVercelDoesNotForwardOpenAIKey(t *testing.T) {
	var requests atomic.Int32
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("VERCEL_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	model, err := New(Config{BaseURL: srv.URL}).LanguageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}

	_, _ = model.DoGenerate(context.Background(), opts)
	if got, _ := auth.Load().(string); requests.Load() > 0 && got == "Bearer openai-secret" {
		t.Fatalf("OPENAI_API_KEY leaked to Vercel: Authorization = %q", got)
	}

	// v0.5.x reads the variable when the provider is built.
	t.Setenv("VERCEL_API_KEY", "vercel-key")
	model, err = New(Config{BaseURL: srv.URL}).LanguageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = model.DoGenerate(context.Background(), opts)
	if got, _ := auth.Load().(string); got != "Bearer vercel-key" {
		t.Fatalf("Authorization = %q, want Bearer vercel-key", got)
	}
}
