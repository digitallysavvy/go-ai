package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r86provbToolInputCases are TS 30548ee7f9 outputs computed under Node from
// literal copies of the converters' serializers, for a Go tool input text
// raw whose TS prompt input is the parsed JSON value, or the raw string when
// it is not JSON (an invalid tool call): serialize (openai-compatible, Groq,
// Cohere, Mistral, DeepSeek, OpenAI Responses), openaiChat (OpenAI Chat
// Completions), stringify (JSON.stringify: xAI, Moonshot, Alibaba) and
// openResponses (string as is).
var r86provbToolInputCases = []struct{ raw, serialize, openaiChat, stringify, openResponses string }{
	{`{"a":1}`, `{"a":1}`, `{"a":1}`, `{"a":1}`, `{"a":1}`},
	{`[1,2]`, `[1,2]`, `{}`, `[1,2]`, `[1,2]`},
	{`"{\"a\":1}"`, `{"a":1}`, `{"a":1}`, `"{\"a\":1}"`, `{"a":1}`},
	{`"[1]"`, `[1]`, `{}`, `"[1]"`, `[1]`},
	{`"123"`, `123`, `{}`, `"123"`, `123`},
	{`"abc"`, `"abc"`, `{}`, `"abc"`, `abc`},
	{`"null"`, `null`, `{}`, `"null"`, `null`},
	{`abc`, `"abc"`, `{}`, `"abc"`, `abc`},
	{`{"a":`, `"{\"a\":"`, `{}`, `"{\"a\":"`, `{"a":`},
	{`123`, `123`, `{}`, `123`, `123`},
	{`null`, `null`, `{}`, `null`, `null`},
	{`true`, `true`, `{}`, `true`, `true`},
	{`"\"x\""`, `"x"`, `{}`, `"\"x\""`, `"x"`},
	{`{"b":"<&>"}`, `{"b":"<&>"}`, `{"b":"<&>"}`, `{"b":"<&>"}`, `{"b":"<&>"}`},
}

func TestR86provbToolCallInputSerializersMatchNode(t *testing.T) {
	for _, tc := range r86provbToolInputCases {
		if got := SerializeToolCallArguments(tc.raw); got != tc.serialize {
			t.Errorf("SerializeToolCallArguments(%s) = %s, want %s", tc.raw, got, tc.serialize)
		}
		if got := StringifyToolCallInput(tc.raw); got != tc.stringify {
			t.Errorf("StringifyToolCallInput(%s) = %s, want %s", tc.raw, got, tc.stringify)
		}
		if got := StringToolCallInput(tc.raw); got != tc.openResponses {
			t.Errorf("StringToolCallInput(%s) = %s, want %s", tc.raw, got, tc.openResponses)
		}
		for _, sanitize := range []bool{false, true} {
			want := tc.serialize
			if sanitize {
				want = tc.openaiChat
			}
			calls := openAIToolCalls([]types.ToolCall{{ID: "c", ToolName: "t", RawArguments: tc.raw}}, sanitize)
			if got := calls[0]["function"].(map[string]interface{})["arguments"]; got != want {
				t.Errorf("openAIToolCalls(%s, sanitize=%v) = %v, want %s", tc.raw, sanitize, got, want)
			}
		}
	}
}
