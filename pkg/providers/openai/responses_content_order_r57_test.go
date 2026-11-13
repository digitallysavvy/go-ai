package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r57PartKinds(parts []types.ContentPart) string {
	kinds := make([]string, 0, len(parts))
	for _, p := range parts {
		switch v := p.(type) {
		case types.ToolCallContent:
			kinds = append(kinds, "call:"+v.ToolName)
		default:
			kinds = append(kinds, strings.TrimPrefix(fmt.Sprintf("%T", p), "types."))
		}
	}
	return strings.Join(kinds, " ")
}

// TS doGenerate pushes every output item's part in output order, so a
// function call ahead of a message stays ahead of its text in the content and
// in the response message sent back on the next step. Go listed client tool
// calls only in ToolCalls, and core appended them after all other content.
func TestR57ResponsesGenerateKeepsToolCallsInOutputOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","model":"gpt-5","output":[` +
			`{"type":"reasoning","id":"rs_1","summary":[]},` +
			`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"{}","status":"completed"},` +
			`{"type":"custom_tool_call","id":"ctc_1","call_id":"call_2","name":"g","input":"x","status":"completed"},` +
			`{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],` +
			`"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gpt-5")
	got, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if have, want := r57PartKinds(got.Content), "ReasoningContent call:f call:g TextContent"; have != want {
		t.Fatalf("DoGenerate content = %q, want %q", have, want)
	}
	if len(got.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(got.ToolCalls))
	}

	res, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msgs := res.FinalStep.Response.Messages
	if len(msgs) == 0 {
		t.Fatal("no response messages")
	}
	if have, want := r57PartKinds(msgs[0].Content), "ReasoningContent call:f call:g TextContent"; !strings.HasPrefix(have, want) {
		t.Fatalf("assistant message content = %q, want it to start with %q", have, want)
	}
}
