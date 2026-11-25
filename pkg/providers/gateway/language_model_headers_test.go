package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS combineHeaders(config.headers, options.headers, model headers): per-call
// headers reach the wire for generate and stream, the model's own headers win
// over them, and generate exposes request/response bodies and headers.
func TestLanguageModelForwardsPerCallHeadersAndBodies(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]http.Header{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("ai-language-model-streaming")] = r.Header.Clone()
		mu.Unlock()
		if r.Header.Get("ai-language-model-streaming") == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"finish\",\"finishReason\":{\"unified\":\"stop\"}}\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Reply", "yes")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi"}],"finishReason":{"unified":"stop"},"usage":{}}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.LanguageModel("openai/gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{
		Prompt:  types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		Headers: map[string]string{"X-Call": "1", "ai-language-model-id": "spoofed"},
	}
	res, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRequest == nil || res.RawResponse == nil || res.ResponseHeaders["X-Reply"] != "yes" && res.ResponseHeaders["x-reply"] != "yes" {
		t.Fatalf("request/response not exposed: req=%v resp=%v headers=%v", res.RawRequest, res.RawResponse, res.ResponseHeaders)
	}
	st, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	for mode, h := range seen {
		if h.Get("X-Call") != "1" {
			t.Errorf("streaming=%s: per-call header not sent", mode)
		}
		if h.Get("ai-language-model-id") != "openai/gpt-5" {
			t.Errorf("streaming=%s: model header must win over per-call, got %q", mode, h.Get("ai-language-model-id"))
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected generate and stream requests, saw %d", len(seen))
	}
}
