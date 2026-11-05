package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A completion stream reports the HTTP response headers through
// provider.StreamResponseHeaders (TS doStream response.headers, round 87e)
// and emits only TS's first-chunk response-metadata part.
func TestR87eintegCompletionStreamHeadersBesideTheStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Test", "v")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"text":"hi","finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	metas := 0
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeResponseMetadata {
			metas++
		}
	}
	if metas != 1 {
		t.Fatalf("response-metadata parts = %d, want TS's one", metas)
	}
	rh, ok := s.(provider.StreamResponseHeaders)
	if !ok || rh.ResponseHeaders()["X-Test"] != "v" {
		t.Fatalf("headers not reported beside the stream (%T)", s)
	}
}
