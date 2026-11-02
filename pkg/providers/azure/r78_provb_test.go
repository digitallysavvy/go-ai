package azure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseJsonEventStream drops a `data: [DONE]` frame and keeps reading:
// frames after it still count and flush() (the finish part) runs at the end
// of the body (coordinator ruling r78 a).
func TestR78provbAzureChatReadsPastDone(t *testing.T) {
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
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.ChatModel("dep")
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

// TS createDeepSeekModel (azure-openai-provider.ts) leaves
// supportsStrictToolCalls unset, so DeepSeekChatLanguageModel sends strict
// tools without the /beta base URL check that createDeepSeek has.
func TestR78provbAzureDeepSeekStrictTools(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.DeepSeek("dep")
	if err != nil {
		t.Fatal(err)
	}
	strict := true
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "x"},
		Tools: []types.Tool{
			{Name: "a", Parameters: map[string]interface{}{"type": "object"}, Strict: &strict},
			{Name: "b", Parameters: map[string]interface{}{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	tools, _ := body["tools"].([]interface{})
	if len(tools) != 2 {
		t.Fatalf("tools = %v", body["tools"])
	}
}
