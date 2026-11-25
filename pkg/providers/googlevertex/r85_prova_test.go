package googlevertex

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

// google-vertex-provider.test.ts async apiKey (TS 7716e189ee): a key func
// enables express mode; the key is resolved per request, sent as
// x-goog-api-key over any header of that name, and must be non-empty.
func TestR85provaVertexExpressAPIKeyFunc(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("x-goog-api-key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}`))
	}))
	defer srv.Close()
	t.Setenv("GOOGLE_VERTEX_API_KEY", "env-key")
	key := "fn-key"
	p, err := New(Config{BaseURL: srv.URL, Headers: map[string]string{"X-Goog-Api-Key": "header-key"}, APIKeyFunc: func(context.Context) (string, error) { return key, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Interactions("gemini-2.5-flash"); err == nil {
		t.Fatal("express mode with a key func must reject Interactions models")
	}
	m, err := p.LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "fn-key" {
		t.Fatalf("x-goog-api-key = %q", keys)
	}
	key = "  "
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var lerr *providererrors.LoadAPIKeyError
	if !errors.As(err, &lerr) || lerr.Message != "Google Vertex API key must be a non-empty string." {
		t.Fatalf("blank key: %v", err)
	}
}
