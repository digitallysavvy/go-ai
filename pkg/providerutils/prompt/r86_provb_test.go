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

// TestR86provbThoughtSignatureNullishFallback: TS reads
// `providerOptions[key]?.thoughtSignature ?? providerOptions.google?.thoughtSignature`
// and sends it when truthy, so an empty signature under the provider key
// does not fall back to the google one.
func TestR86provbThoughtSignatureNullishFallback(t *testing.T) {
	convert := func(po map[string]interface{}) map[string]interface{} {
		msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: `{}`, ProviderOptions: po},
		}}}
		out := ToOpenAIMessages(msgs, ToOpenAIMessagesOptions{ThoughtSignatureKey: "ollama"})
		return out[0]["tool_calls"].([]map[string]interface{})[0]
	}
	call := convert(map[string]interface{}{"ollama": map[string]interface{}{"thoughtSignature": ""}, "google": map[string]interface{}{"thoughtSignature": "g"}})
	if _, has := call["extra_content"]; has {
		t.Fatalf("empty ollama signature fell back to google: %v", call)
	}
	call = convert(map[string]interface{}{"ollama": map[string]interface{}{"thoughtSignature": nil}, "google": map[string]interface{}{"thoughtSignature": "g"}})
	if got := call["extra_content"]; got == nil {
		t.Fatalf("null ollama signature did not fall back to google: %v", call)
	}
}
