package xai

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

func r58PartKinds(parts []types.ContentPart) string {
	kinds := make([]string, 0, len(parts))
	for _, p := range parts {
		switch v := p.(type) {
		case types.ToolCallContent:
			kinds = append(kinds, "call:"+v.ToolName)
		case types.ToolResultContent:
			kinds = append(kinds, "result:"+v.ToolName)
		default:
			kinds = append(kinds, strings.TrimPrefix(fmt.Sprintf("%T", p), "types."))
		}
	}
	return strings.Join(kinds, " ")
}

// TS xai-responses-language-model.ts doGenerate pushes every output item's
// parts in output order: a function_call ahead of a message stays ahead of its
// text, and a provider-executed call (image_generation_call, web_search_call,
// file_search_call) comes right before its tool-result. Go listed every call
// only in ToolCalls, so core appended them after all other content.
func TestR58XAIResponsesGenerateKeepsToolCallsInOutputOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","model":"grok-4","output":[` +
			`{"type":"image_generation_call","id":"ig_1","status":"completed","result":"aGk="},` +
			`{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"q"}},` +
			`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"{}","status":"completed"},` +
			`{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],` +
			`"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "grok-4")
	want := "call:image_generation result:image_generation call:web_search result:web_search call:f TextContent"

	got, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if have := r58PartKinds(got.Content); have != want {
		t.Fatalf("DoGenerate content = %q, want %q", have, want)
	}
	if len(got.ToolCalls) != 3 {
		t.Fatalf("tool calls = %d, want 3", len(got.ToolCalls))
	}
	if got.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want tool-calls", got.FinishReason)
	}

	res, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msgs := res.FinalStep.Response.Messages
	if len(msgs) == 0 {
		t.Fatal("no response messages")
	}
	if have := r58PartKinds(msgs[0].Content); !strings.HasPrefix(have, want) {
		t.Fatalf("assistant message content = %q, want it to start with %q", have, want)
	}
}
