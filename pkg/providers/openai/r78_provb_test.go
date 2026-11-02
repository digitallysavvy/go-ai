package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r78provbSSE serves the given SSE frames and then ends the body.
func r78provbSSE(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r78provbDrain(t *testing.T, s provider.TextStream) (string, []*provider.StreamChunk) {
	t.Helper()
	defer s.Close()
	var text strings.Builder
	var finishes []*provider.StreamChunk
	var last *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		last = c
		switch c.Type {
		case provider.ChunkTypeText:
			text.WriteString(c.Text)
		case provider.ChunkTypeFinish:
			finishes = append(finishes, c)
		case provider.ChunkTypeError:
			t.Fatalf("unexpected error part: %+v", c)
		}
	}
	if len(finishes) != 1 || last != finishes[0] {
		t.Fatalf("want exactly one finish part, last; got %d finishes, last %+v", len(finishes), last)
	}
	return text.String(), finishes
}

// TS parseJsonEventStream drops a `data: [DONE]` frame and keeps reading:
// frames after it still count and flush() (the finish part) runs at the end
// of the body (coordinator ruling r78 a).
func TestR78provbChatReadsPastDone(t *testing.T) {
	srv := r78provbSSE(t,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
		`[DONE]`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
	)
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	text, fin := r78provbDrain(t, s)
	if text != "Hello world" || fin[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("text %q finish %q, want %q stop", text, fin[0].FinishReason, "Hello world")
	}
}

func TestR78provbCompletionReadsPastDone(t *testing.T) {
	srv := r78provbSSE(t,
		`{"id":"c1","object":"text_completion","created":1,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"Hello","finish_reason":null}]}`,
		`[DONE]`,
		`{"id":"c1","object":"text_completion","created":1,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":" world","finish_reason":"stop"}]}`,
	)
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	text, fin := r78provbDrain(t, s)
	if text != "Hello world" || fin[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("text %q finish %q, want %q stop", text, fin[0].FinishReason, "Hello world")
	}
}
