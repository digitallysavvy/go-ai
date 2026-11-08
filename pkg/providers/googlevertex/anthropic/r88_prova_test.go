package anthropic

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Credential transports (S2, round 88 sweep, related to TS 977231e913): the
// Google token and custom headers go only to the Vertex Anthropic origin; a
// cross-origin redirect hop receives neither, and a foreign request never
// asks for a token.
func TestR88provaVertexAnthropicCredentialsSameOriginOnly(t *testing.T) {
	base, foreign, api := r88provaRedirectServers(t)
	var tokens int32
	p := New(Options{
		BaseURL:   base,
		AuthToken: func(context.Context) (string, error) { atomic.AddInt32(&tokens, 1); return "tok", nil },
		Headers:   map[string]string{"X-Custom-Header": "secret"},
	})
	m, err := p.LanguageModel("claude-sonnet-4-5@20250929")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if len(api()) != 1 || api()[0].Get("Authorization") != "Bearer tok" || api()[0].Get("X-Custom-Header") != "secret" {
		t.Fatalf("API request headers = %v", api())
	}
	if len(foreign()) != 1 {
		t.Fatalf("foreign requests = %d", len(foreign()))
	}
	for _, h := range r88provaCredentialHeaders {
		if v := foreign()[0].Get(h); v != "" {
			t.Fatalf("foreign origin received %s: %q", h, v)
		}
	}
	if tokens != 1 {
		t.Fatalf("token func calls = %d", tokens)
	}

	// a foreign URL through the transport directly
	tr := p.httpClient(base).Transport
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/image.png", nil)
	rec := &r88provaRecordingRT{}
	tr.(*authTransport).base = rec
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if rec.header.Get("Authorization") != "" || rec.header.Get("X-Custom-Header") != "" || tokens != 1 {
		t.Fatalf("foreign request headers = %v, token calls = %d", rec.header, tokens)
	}
}

type r88provaRecordingRT struct{ header http.Header }

func (r *r88provaRecordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.header = req.Header.Clone()
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

// TS 6b101ac076: Vertex Anthropic rejects message threads before a request.
func TestR88provaVertexAnthropicRejectsThreads(t *testing.T) {
	p := New(Options{BaseURL: "http://127.0.0.1:1", AuthToken: func(context.Context) (string, error) { return "tok", nil }})
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "create"}}}})
	if err == nil || err.Error() != "googleVertex.anthropic.messages does not support Anthropic message threads." {
		t.Fatalf("err = %v", err)
	}
}
