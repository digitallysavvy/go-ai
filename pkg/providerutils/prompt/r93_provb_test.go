package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS openai-compatible: `thoughtSignature ? {extra_content: {google:
// {thought_signature: String(thoughtSignature)}}} : {}` -- a falsy value
// sends nothing, a number is its JS text.
func TestR93provbThoughtSignatureTruthiness(t *testing.T) {
	for sig, want := range map[interface{}]interface{}{false: nil, 0.0: nil, "": nil, 1e21: "1e+21", "abc": "abc"} {
		msg := types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
			ToolCallID: "c1", ToolName: "t", Input: "{}",
			ProviderOptions: map[string]interface{}{"together": map[string]interface{}{"thoughtSignature": sig}},
		}}}
		out := ToOpenAIMessages([]types.Message{msg}, ToOpenAIMessagesOptions{ThoughtSignatureKey: "together"})
		calls := out[0]["tool_calls"].([]map[string]interface{})
		extra, has := calls[0]["extra_content"]
		if want == nil {
			if has {
				t.Errorf("%v: extra_content = %v, want none", sig, extra)
			}
			continue
		}
		got := extra.(map[string]interface{})["google"].(map[string]interface{})["thought_signature"]
		if got != want {
			t.Errorf("%v: signature = %v, want %v", sig, got, want)
		}
	}
}
