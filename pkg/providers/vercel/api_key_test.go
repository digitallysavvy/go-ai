package vercel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

	_, err = model.DoGenerate(context.Background(), opts)
	if !providererrors.IsLoadAPIKeyError(err) || requests.Load() != 0 {
		t.Fatalf("OPENAI_API_KEY leaked to Vercel: err=%v requests=%d", err, requests.Load())
	}

	t.Setenv("VERCEL_API_KEY", "vercel-key")
	_, _ = model.DoGenerate(context.Background(), opts)
	if got, _ := auth.Load().(string); got != "Bearer vercel-key" {
		t.Fatalf("Authorization = %q, want Bearer vercel-key", got)
	}
}
