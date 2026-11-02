package groq

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r78provagroqServer(t *testing.T, body string, sse bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TS createLanguageModelResponseMetadata: created != null ? new Date(created * 1000),
// so created: 0 is the epoch and a fractional created keeps its milliseconds.
func TestR78prova_groqCreatedTimestamp(t *testing.T) {
	for _, tc := range []struct {
		created string
		want    time.Time
	}{
		{"0", time.Unix(0, 0)},
		{"1711115037.5", time.UnixMilli(1711115037500)},
		{"null", time.Time{}},
	} {
		body := `{"id":"x","object":"chat.completion","created":` + tc.created + `,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		m, err := New(Config{APIKey: "k", BaseURL: r78provagroqServer(t, body, false)}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatalf("created %s: %v", tc.created, err)
		}
		if res.ResponseMetadata == nil || !res.ResponseMetadata.Timestamp.Equal(tc.want) {
			t.Fatalf("generate created %s: metadata %+v, want timestamp %v", tc.created, res.ResponseMetadata, tc.want)
		}

		sse := "data: " + `{"id":"x","object":"chat.completion.chunk","created":` + tc.created + `,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}` + "\n\n" +
			"data: " + `{"id":"x","object":"chat.completion.chunk","created":` + tc.created + `,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
		m, err = New(Config{APIKey: "k", BaseURL: r78provagroqServer(t, sse, true)}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		var got *provider.ResponseMetadata
		for {
			c, err := stream.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeError {
				t.Fatalf("stream created %s: error chunk %v", tc.created, c.Err)
			}
			if c.Type == provider.ChunkTypeResponseMetadata && got == nil {
				got = c.ResponseMetadata
			}
		}
		_ = stream.Close()
		if got == nil || !got.Timestamp.Equal(tc.want) {
			t.Fatalf("stream created %s: metadata %+v, want timestamp %v", tc.created, got, tc.want)
		}
	}
}

// TS parseJsonEventStream drops `data: [DONE]` and keeps reading: frames
// after it are processed and the finish comes at the end of the body.
func TestR78prova_groqReadsPastDone(t *testing.T) {
	sse := "data: " + `{"id":"x","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"a"},"finish_reason":null}]}` + "\n\ndata: [DONE]\n\n" +
		"data: " + `{"id":"x","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"b"},"finish_reason":"stop"}]}` + "\n\n"
	m, err := New(Config{APIKey: "k", BaseURL: r78provagroqServer(t, sse, true)}).LanguageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text, finishes, last := "", 0, provider.ChunkType("")
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeText {
			text += c.Text
		}
		if c.Type == provider.ChunkTypeFinish {
			finishes++
		}
		last = c.Type
	}
	if text != "ab" || finishes != 1 || last != provider.ChunkTypeFinish {
		t.Fatalf("text %q, finishes %d, last %s; want \"ab\", 1 finish last", text, finishes, last)
	}
}
