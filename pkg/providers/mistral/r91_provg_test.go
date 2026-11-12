package mistral

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

// r91provgCapture serves a fixed body and records the request body.
func r91provgCapture(t *testing.T, contentType, resp string) (*httptest.Server, *[]byte) {
	t.Helper()
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

// provg2-01: a System-only prompt must not panic; TS
// convert-to-mistral-chat-messages.ts:54 sends [{role:system}].
func TestR91provgSystemOnlyPrompt(t *testing.T) {
	server, got := r91provgCapture(t, "application/json", `{"id":"x","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("mistral-small-latest")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{System: "be brief"}}); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages json.RawMessage `json:"messages"`
	}
	_ = json.Unmarshal(*got, &body)
	if string(body.Messages) != `[{"content":"be brief","role":"system"}]` {
		t.Fatalf("messages = %s", body.Messages)
	}
}
