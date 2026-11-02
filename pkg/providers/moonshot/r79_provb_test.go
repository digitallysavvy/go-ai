package moonshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS records finish_reason when it is != null, so an empty one maps to
// "other" with raw "" (Go read "" as no finish_reason: interrupted).
func TestR79provbMoonshotEmptyFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":""}]}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonOther || finish.RawFinishReason != "" {
		t.Fatalf("finish = %+v", finish)
	}
}
