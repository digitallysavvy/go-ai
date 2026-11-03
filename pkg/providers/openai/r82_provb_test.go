package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r82provbStreamParts streams body from a test server through an
// OpenAI-compatible chat model and returns the chunks read.
func r82provbStreamParts(t *testing.T, body string) []*provider.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "test-provider"}).ChatModel("m")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		out = append(out, c)
	}
	return out
}

// TS openai-compatible pendingToolCalls keeps a present empty id (`id ??
// null`), so the later "real" id is not adopted and the call gets a
// generated id; and a never-named pending call throws in flush() before
// toolCallTracker.flush(), so the named call is not finalized and no
// finish follows.
func TestR82provbCompatPendingIDAndFlushThrow(t *testing.T) {
	parts := r82provbStreamParts(t, `data: {"id":"x","choices":[{"delta":{"tool_calls":[{"index":0,"id":"","function":{"arguments":"{"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"real","function":{"name":"t","arguments":"}"}}]},"finish_reason":"tool_calls"}]}

`)
	var call *types.ToolCall
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolCall {
			call = p.ToolCall
		}
	}
	if call == nil || call.ID == "real" || call.ID == "" || call.RawArguments != "{}" {
		t.Fatalf("tool call = %+v, want a generated id (not \"real\") and {}", call)
	}

	parts = r82provbStreamParts(t, `data: {"id":"x","choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","function":{"arguments":"{}"}},{"index":1,"id":"c1","function":{"name":"t","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

`)
	var types_ []provider.ChunkType
	for _, p := range parts {
		types_ = append(types_, p.Type)
		if p.Type == provider.ChunkTypeToolCall || p.Type == provider.ChunkTypeFinish {
			t.Fatalf("unexpected %s after a never-named pending call: %v", p.Type, types_)
		}
	}
	if last := parts[len(parts)-1]; last.Type != provider.ChunkTypeError || !strings.Contains(last.Text, "function.name") {
		t.Fatalf("last part = %+v, want the function.name error", last)
	}
}
