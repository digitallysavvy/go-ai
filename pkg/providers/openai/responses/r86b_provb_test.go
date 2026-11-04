package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutil "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// TestR86bprovbFunctionCallArgumentsRestringified pins the Responses
// function_call arguments of a replayed tool call to TS
// serializeToolCallArguments (convert-to-openai-responses-input.ts): a Go
// input text that is valid JSON is the TS parsed input, sent as
// JSON.stringify of it; other text (whitespace only, JSON behind U+00A0) is
// the TS string input (V061 item 28). Each want was printed by Node 24 from a
// literal copy of the TS function.
func TestR86bprovbFunctionCallArgumentsRestringified(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"{\"b\": 1, \"a\": [1.0, 2e0]}", "{\"b\":1,\"a\":[1,2]}"},
		{"{\"2\":\"x\",\"1\":\"y\",\"k\":-0}", "{\"1\":\"y\",\"2\":\"x\",\"k\":0}"},
		{"[1, {\"z\": 1, \"y\": 2.50}]", "[1,{\"z\":1,\"y\":2.5}]"},
		{"\"{\\\"q\\\": 1.0}\"", "{\"q\": 1.0}"},
		{"\"\\u0061bc\"", "\"abc\""},
		{"not json", "\"not json\""},
		{"   ", "\"   \""},
		{"\u00a0{\"a\":1}", "\"\u00a0{\\\"a\\\":1}\""},
		{"12.0", "12"},
		{"{\"a\":1,\"a\":2}", "{\"a\":2}"},
		{" {\"a\" : 1e2} ", "{\"a\":100}"},
	} {
		if got := responsesFunctionCallArguments(nil, tc.raw); got != tc.want {
			t.Errorf("tool call %q: arguments = %q, want %q", tc.raw, got, tc.want)
		}
		_, got := toolCallContentArguments(types.ToolCallContent{Input: tc.raw})
		if text, _, ok := promptutil.JSONStringToolInput(tc.raw); ok {
			got = text
		}
		if got != tc.want {
			t.Errorf("tool-call part %q: arguments = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
