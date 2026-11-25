package xai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r91provgServer(t *testing.T) (*httptest.Server, func() (int, http.Header, string)) {
	t.Helper()
	var mu sync.Mutex
	hits := 0
	var hdr http.Header
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits++
		hdr = r.Header.Clone()
		body = string(b)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() (int, http.Header, string) { mu.Lock(); defer mu.Unlock(); return hits, hdr, body }
}

// provg1-12: the provider's Config.Headers win over call headers (TS
// google-vertex-xai-provider-node.ts:40 customFetch).
func TestR91provgProviderHeadersWin(t *testing.T) {
	srv, got := r91provgServer(t)
	p, _ := New(Config{Project: "p", Location: "global", AccessToken: "t", BaseURL: srv.URL, Headers: map[string]string{"X-A": "p"}})
	m, _ := p.LanguageModel(ModelGrok41FastReasoning)
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: map[string]string{"X-A": "c"}}); err != nil {
		t.Fatal(err)
	}
	if _, h, _ := got(); h.Get("X-A") != "p" || h.Get("Authorization") != "Bearer t" {
		t.Fatalf("headers = %v", h)
	}
}

// core2-24: openai-compatible chat options are validated (TS
// openai-compatible-chat-language-model.ts:229) and no request is sent.
func TestR91provgValidatesCompatibleOptions(t *testing.T) {
	srv, got := r91provgServer(t)
	p, _ := New(Config{Project: "p", Location: "global", AccessToken: "t", BaseURL: srv.URL})
	m, _ := p.LanguageModel(ModelGrok41FastReasoning)
	for _, opts := range []map[string]interface{}{{"openaiCompatible": map[string]interface{}{"user": 123}}, {"googleVertex": map[string]interface{}{"user": 123}}} {
		_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: opts})
		if err == nil || !strings.Contains(err.Error(), "provider options") {
			t.Fatalf("%v: error = %v", opts, err)
		}
		if _, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: opts}); err == nil {
			t.Fatalf("%v: stream accepted", opts)
		}
	}
	if n, _, _ := got(); n != 0 {
		t.Fatalf("requests sent: %d", n)
	}
}
