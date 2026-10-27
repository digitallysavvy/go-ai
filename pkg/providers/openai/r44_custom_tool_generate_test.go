package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The generate path decodes a custom_tool_call like the stream path: TS
// `input: JSON.stringify(part.input)` as RawArguments.
func TestR44_CustomToolCallGenerateRawArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","model":"gpt-5","output":[{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"format_code","input":"print(\"1\")\n","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gpt-5")
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	if got := result.ToolCalls[0].RawArguments; got != `"print(\"1\")\n"` {
		t.Fatalf("RawArguments = %q", got)
	}
}
