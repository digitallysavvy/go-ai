package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86bprovbToolCallArgumentsRestringified pins the OpenAI chat
// serializeToolCallArguments and the JSON.stringify(part.input) converters
// (xAI Responses, Moonshot, Alibaba) to TS: a Go tool input text that is
// valid JSON is the TS parsed input and is re-stringified (V061 item 28).
// Each want was printed by Node 24 from literal copies of the TS serializers
// (convert-to-openai-chat-messages.ts serializeToolCallArguments; JSON.stringify).
func TestR86bprovbToolCallArgumentsRestringified(t *testing.T) {
	for _, tc := range []struct{ raw, openaiChat, stringify string }{
		{"{\"b\": 1, \"a\": [1.0, 2e0]}", "{\"b\":1,\"a\":[1,2]}", "{\"b\":1,\"a\":[1,2]}"},
		{"{\"2\":\"x\",\"1\":\"y\",\"k\":-0}", "{\"1\":\"y\",\"2\":\"x\",\"k\":0}", "{\"1\":\"y\",\"2\":\"x\",\"k\":0}"},
		{"[1, {\"z\": 1, \"y\": 2.50}]", "{}", "[1,{\"z\":1,\"y\":2.5}]"},
		{"\"{\\\"q\\\": 1.0}\"", "{\"q\": 1.0}", "\"{\\\"q\\\": 1.0}\""},
		{"\"\\u0061bc\"", "{}", "\"abc\""},
		{"not json", "{}", "\"not json\""},
		{"   ", "{}", "\"   \""},
		{"\u00a0{\"a\":1}", "{}", "\"\u00a0{\\\"a\\\":1}\""},
		{"12.0", "{}", "12"},
		{"{\"a\":1,\"a\":2}", "{\"a\":2}", "{\"a\":2}"},
		{" {\"a\" : 1e2} ", "{\"a\":100}", "{\"a\":100}"},
	} {
		calls := openAIToolCalls([]types.ToolCall{{ID: "c", ToolName: "t", RawArguments: tc.raw}}, true)
		fn := calls[0]["function"].(map[string]interface{})
		if got := fn["arguments"]; got != tc.openaiChat {
			t.Errorf("OpenAI chat arguments for %q = %q, want %q", tc.raw, got, tc.openaiChat)
		}
		if got := StringifyToolCallInput(tc.raw); got != tc.stringify {
			t.Errorf("StringifyToolCallInput(%q) = %q, want %q", tc.raw, got, tc.stringify)
		}
	}
}
