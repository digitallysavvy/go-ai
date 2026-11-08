package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r88provbChatStream serves frames as an SSE body to a chat model and returns
// every stream part.
func r88provbChatStream(t *testing.T, frames []string, compat bool) []*provider.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	cfg := Config{APIKey: "k", BaseURL: srv.URL}
	if compat {
		cfg.OpenAICompatible, cfg.Name = true, "test-provider"
	}
	m, err := New(cfg).ChatModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var out []*provider.StreamChunk
	for {
		c, err := st.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
}

// TS f6dbf04b5d (openai-chat-language-model.ts doStream): an empty
// `content: ""` delta is emitted only when the chunk has no other text delta;
// a refusal in the same chunk replaces it.
func TestR88provbChatStreamEmptyContentWithRefusal(t *testing.T) {
	parts := r88provbChatStream(t, []string{
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"","refusal":"No."},"finish_reason":null}]}`,
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}, false)
	var deltas []string
	var finish *provider.StreamChunk
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeText:
			deltas = append(deltas, p.Text)
		case provider.ChunkTypeFinish:
			finish = p
		}
	}
	if len(deltas) != 1 || deltas[0] != "No." {
		t.Fatalf("text deltas = %q, want [\"No.\"]", deltas)
	}
	if finish == nil || finish.FinishReason != "content-filter" || finish.RawFinishReason != "stop" {
		t.Fatalf("finish = %+v, want content-filter / stop", finish)
	}

	// Without a refusal the empty content delta keeps its v0.5 behavior.
	parts = r88provbChatStream(t, []string{
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}, false)
	deltas = nil
	for _, p := range parts {
		if p.Type == provider.ChunkTypeText {
			deltas = append(deltas, p.Text)
		}
	}
	if len(deltas) != 1 || deltas[0] != "" {
		t.Fatalf("text deltas = %q, want [\"\"]", deltas)
	}
}
