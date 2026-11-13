package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A tool input that is null behind Unicode spaces is not JSON: TS gets it as
// the string input and sends {rawInvalidInput: "<text>"} (toAnthropicToolInput;
// secureJsonParse fails on U+00A0 / U+0085); JSON whitespace around null is
// still the null input.
func TestR86bprovaAnthropicNullToolInputJSONWhitespace(t *testing.T) {
	for input, want := range map[string]string{
		"\u00a0null":    `{"rawInvalidInput":"\u00a0null"}`,
		"null\u0085":    `{"rawInvalidInput":"null\u0085"}`,
		" \t\r\nnull\n": `{"rawInvalidInput":null}`,
	} {
		for _, part := range []types.ContentPart{
			types.ToolCallContent{ToolCallID: "t1", ToolName: "x", Input: input},
		} {
			p, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{part}}}, AnthropicPromptOptions{})
			if err != nil {
				t.Fatal(err)
			}
			assertJSONEqual(t, "messages", p.Messages, `[{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"x","input":`+want+`}]}]`)
		}
		p, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "t1", ToolName: "x", RawArguments: input}}}}, AnthropicPromptOptions{})
		if err != nil {
			t.Fatal(err)
		}
		assertJSONEqual(t, "legacy messages", p.Messages, `[{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"x","input":`+want+`}]}]`)
	}
}
