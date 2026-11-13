package alibaba

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS records finish_reason when it is != null, so an empty one maps to
// "other" with raw "" (Go read "" as no finish_reason: interrupted).
func TestR79provbAlibabaEmptyFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":""}]}` + "\n\ndata: [DONE]\n\n"))
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

// A text prompt with a system prompt keeps both messages: the user text used
// to be dropped whenever System was set.
func TestR79provbAlibabaSystemAndTextPrompt(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "qwen-plus")
	body, _, err := m.buildRequestBodyChecked(&provider.GenerateOptions{Prompt: types.Prompt{System: "be brief", Text: "hi"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body["messages"])
	const want = `[{"content":"be brief","role":"system"},{"content":[{"text":"hi","type":"text"}],"role":"user"}]`
	if string(b) != want {
		t.Fatalf("messages = %s, want %s", b, want)
	}
}
