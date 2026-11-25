// Target: pkg/providers/bedrock/r89_carry_redirect_test.go (prova)
package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A redirect to another port of the same host must not carry the Bearer API
// key (fetch drops Authorization on any cross-origin hop). Covers the
// converse (generate and stream) and embedding sites; image and reranking
// use the same call.
func TestR89carryRedirectDropsAuthorization(t *testing.T) {
	var mu sync.Mutex
	var got []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Path+"="+r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"stop"}`))
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	p := New(Config{APIKey: "k", Region: "us-east-1", BaseURL: origin.URL})
	lm, err := p.LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}
	_, _ = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt})
	if s, err := lm.DoStream(context.Background(), &provider.GenerateOptions{Prompt: prompt}); err == nil && s != nil {
		_ = s.Close()
	}
	em, err := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = em.DoEmbed(context.Background(), "hi", nil)
	mu.Lock()
	defer mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("expected 3 redirected requests, got %v", got)
	}
	for _, g := range got {
		if g[len(g)-1] != '=' {
			t.Fatalf("other origin got Authorization: %v", got)
		}
	}
}
