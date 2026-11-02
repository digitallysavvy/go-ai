package streaming_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

const r78provbDoneBody = `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}` + "\n\n" +
	"data: [DONE]\n\n" +
	`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}` + "\n\n"

func r78provbCheckDone(t *testing.T, s provider.TextStream) {
	t.Helper()
	defer func() { _ = s.Close() }()
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

// TS parseJsonEventStream drops a `data: [DONE]` frame and keeps reading;
// flush() (the finish part) runs at the end of the body (ruling r78 a).
func TestR78provbCompatStreamReadsPastDone(t *testing.T) {
	s := streaming.NewOpenAICompatStream(io.NopCloser(strings.NewReader(r78provbDoneBody)), providerutils.MapOpenAIFinishReason)
	r78provbCheckDone(t, s)
}

// The same through the providers that embed OpenAICompatStream.
func TestR78provbCompatEmbeddersReadPastDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(r78provbDoneBody))
	}))
	defer srv.Close()
	models := map[string]provider.LanguageModel{
		"fireworks": fireworks.NewLanguageModel(fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}), "m"),
		"together":  together.NewLanguageModel(together.New(together.Config{APIKey: "k", BaseURL: srv.URL}), "m"),
		"zai":       zai.NewLanguageModel(zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}), "m"),
		"gmicloud":  gmicloud.NewLanguageModel(gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}), "m"),
		"ollama":    ollama.NewLanguageModel(ollama.New(ollama.Config{BaseURL: srv.URL}), "m"),
	}
	for name, m := range models {
		t.Run(name, func(t *testing.T) {
			s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			r78provbCheckDone(t, s)
		})
	}
}
