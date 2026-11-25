package together

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertToOpenAICompatibleChatMessages sends a text/* file part as a text
// part; the request body must carry it the same way, not as a file part.
func TestTextFilePartIsSentAsTextLikeOpenAICompatible(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "m")
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{
			types.TextContent{Text: "read this"},
			types.FileContent{Data: []byte("hello file"), MediaType: "text/plain"},
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil || len(req.Messages) == 0 {
		t.Fatalf("body = %s (%v)", body, err)
	}
	content := string(req.Messages[len(req.Messages)-1].Content)
	if !strings.Contains(content, `{"text":"hello file","type":"text"}`) && !strings.Contains(content, `{"type":"text","text":"hello file"}`) {
		t.Fatalf("text file part not sent as text: %s", content)
	}
}
