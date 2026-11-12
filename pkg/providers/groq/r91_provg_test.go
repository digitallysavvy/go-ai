package groq

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// provg2-01: a System-only prompt must not panic; TS
// convert-to-groq-chat-messages.ts:33 sends [{role:system}].
func TestR91provgSystemOnlyPrompt(t *testing.T) {
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("llama-3.3-70b-versatile")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{System: "be brief"}}); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages json.RawMessage `json:"messages"`
	}
	_ = json.Unmarshal(got, &body)
	if string(body.Messages) != `[{"content":"be brief","role":"system"}]` {
		t.Fatalf("messages = %s", body.Messages)
	}
}
