package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR85integOpenAIToolCallArguments: TS 4ae6986cd8 serializeToolCallArguments
// in convert-to-openai-compatible-chat-messages.ts (also groq and cohere,
// which use ToOpenAIMessages) and convert-to-openai-chat-messages.ts
// (SanitizeReplayedToolCallArguments). A Go RawArguments that is a JSON
// string literal is the TS string input; other raw text is the TS input's
// JSON (or, when it is not JSON, the TS string itself).
func TestR85integOpenAIToolCallArguments(t *testing.T) {
	for _, c := range []struct{ raw, compat, openai string }{
		{`"{\"b\":2,\"a\":1}"`, `{"b":2,"a":1}`, `{"b":2,"a":1}`},
		{`"{\"a\":"`, `"{\"a\":"`, `{}`},
		{`{"a":`, `"{\"a\":"`, `{}`},
		{`"[1]"`, `[1]`, `{}`},
		{`{"a":1}`, `{"a":1}`, `{"a":1}`},
		{`[1]`, `[1]`, `{}`},
		{`"not json"`, `"not json"`, `{}`},
		{`not json`, `"not json"`, `{}`},
		{`"{\"__proto__\":{}}"`, `"{\"__proto__\":{}}"`, `{}`},
	} {
		calls := []types.ToolCall{{ID: "c", ToolName: "t", RawArguments: c.raw}}
		if got := openAIToolCalls(calls, false)[0]["function"].(map[string]interface{})["arguments"]; got != c.compat {
			t.Errorf("compat %s: arguments = %s, want %s", c.raw, got, c.compat)
		}
		if got := openAIToolCalls(calls, true)[0]["function"].(map[string]interface{})["arguments"]; got != c.openai {
			t.Errorf("openai %s: arguments = %s, want %s", c.raw, got, c.openai)
		}
	}
}
