package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS google-language-model reads its body through parseJsonEventStream, which
// drops `data: [DONE]` and keeps reading; flush (the finish) runs at the end.
func TestR78prova_GoogleReadsPastDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w,
			"data: "+`{"candidates":[{"content":{"parts":[{"text":"a"}],"role":"model"},"index":0}]}`+"\n\ndata: [DONE]\n\n"+
				"data: "+`{"candidates":[{"content":{"parts":[{"text":"b"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`+"\n\n")
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
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
