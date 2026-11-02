package alibaba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseJsonEventStream drops a `data: [DONE]` frame and keeps reading:
// frames after it still count and flush() (the finish part) runs at the end
// of the body (coordinator ruling r78 a).
func TestR78provbAlibabaReadsPastDone(t *testing.T) {
	frames := []string{
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
		`[DONE]`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
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
	if text.String() != "Hello world" || finishes[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("text %q finish %q, want %q stop", text.String(), finishes[0].FinishReason, "Hello world")
	}
}

// r78provbDropMetadata checks the stream carried exactly one
// response-metadata part (TS emits it for the first valid chunk even when
// the chunk has no id, model or created) and returns the other parts.
func r78provbDropMetadata(t *testing.T, chunks []*provider.StreamChunk) []*provider.StreamChunk {
	t.Helper()
	var out []*provider.StreamChunk
	n := 0
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeResponseMetadata {
			n++
			continue
		}
		out = append(out, c)
	}
	if n != 1 {
		t.Fatalf("want exactly one response-metadata part, got %d", n)
	}
	return out
}

// Reviewer item 3 (r78): TS createLanguageModelResponseMetadata uses
// `created != null ? new Date(created * 1000)`, so created 0 is the epoch in
// generate and in the stream's response-metadata; an absent created is none.
func TestR78provbAlibabaCreatedZeroIsEpoch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		created string
		want    bool
	}{{"zero", `"created":0,`, true}, {"absent", ``, false}} {
		check := func(t *testing.T, ts time.Time) {
			t.Helper()
			if tc.want && (ts.IsZero() || !ts.Equal(time.Unix(0, 0))) {
				t.Fatalf("timestamp = %v, want the epoch", ts)
			}
			if !tc.want && !ts.IsZero() {
				t.Fatalf("timestamp = %v, want none", ts)
			}
		}
		t.Run("generate/"+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion",` + tc.created + `"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus")
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			if r.ResponseMetadata == nil {
				t.Fatal("no response metadata")
			}
			check(t, r.ResponseMetadata.Timestamp)
		})
		t.Run("stream/"+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"id":"c1","object":"chat.completion.chunk",` + tc.created + `"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus")
			if err != nil {
				t.Fatal(err)
			}
			s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var meta *provider.ResponseMetadata
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeResponseMetadata && c.ResponseMetadata != nil && c.ResponseMetadata.ID == "c1" {
					meta = c.ResponseMetadata
				}
			}
			if meta == nil {
				t.Fatal("no response-metadata part")
			}
			check(t, meta.Timestamp)
		})
	}
}
