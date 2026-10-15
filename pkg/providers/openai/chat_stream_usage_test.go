package openai

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

func TestChatStreamReturnsTrailingUsage(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"c1","model":"gpt-4o","created":1,"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"!"},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8,"completion_tokens_details":{"reasoning_tokens":1}}}`,
		`data: [DONE]`, ``,
	}, "\n\n")
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	var chunks []*provider.StreamChunk
	for {
		c, err := st.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, c)
	}

	if so, _ := body["stream_options"].(map[string]interface{}); so["include_usage"] != true {
		t.Fatalf("stream_options = %v", body["stream_options"])
	}
	var text string
	var finish *provider.StreamChunk
	for i, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeFinish:
			if finish != nil || i != len(chunks)-1 {
				t.Fatalf("finish must be single and last: %d/%d", i, len(chunks))
			}
			finish = c
		}
	}
	if text != "Hi!" || finish == nil {
		t.Fatalf("text=%q finish=%v", text, finish)
	}
	if finish.FinishReason != types.FinishReasonStop || finish.Usage == nil || *finish.Usage.InputTokens != 5 || *finish.Usage.OutputDetails.ReasoningTokens != 1 {
		t.Fatalf("finish = %+v usage=%+v", finish, finish.Usage)
	}
}

// OpenAI-compatible wrappers (TS createOpenAICompatible without includeUsage)
// must not send stream_options.
func TestChatStreamOmitStreamUsage(t *testing.T) {
	for _, omit := range []bool{false, true} {
		var body map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		}))
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL, OmitStreamUsage: omit}).ChatModel("gpt-4o")
		if err != nil {
			t.Fatal(err)
		}
		st, err := m.DoStream(context.Background(), &provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = st.Close()
		srv.Close()
		if _, has := body["stream_options"]; has == omit {
			t.Fatalf("omit=%v but stream_options present=%v", omit, has)
		}
	}
}
