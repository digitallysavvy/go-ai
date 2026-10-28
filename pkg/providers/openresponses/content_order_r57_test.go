package openresponses

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doGenerate pushes every output item's part in output order, so a
// function call ahead of a message stays ahead of its text. Go listed the
// calls only in ToolCalls, and core appended them after the text.
func TestR57OpenResponsesGenerateKeepsToolCallsInOutputOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[` +
			`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"{}"},` +
			`{"type":"custom_tool_call","id":"ctc_1","call_id":"call_2","name":"g","input":"x"},` +
			`{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],` +
			`"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{BaseURL: srv.URL, Name: "open-responses"}), "local-model")
	got, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(got.Content))
	for _, p := range got.Content {
		if tc, ok := p.(types.ToolCallContent); ok {
			kinds = append(kinds, "call:"+tc.ToolName+":"+tc.Input)
			continue
		}
		kinds = append(kinds, fmt.Sprintf("%T", p))
	}
	if have, want := strings.Join(kinds, " "), `call:f:{} call:g:"x" types.TextContent`; have != want {
		t.Fatalf("content = %q, want %q", have, want)
	}
	if len(got.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(got.ToolCalls))
	}
}
