package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convert-to-anthropic-prompt.ts reasoning parts: a present
// providerOptions.anthropic.signature (`reasoningMetadata.signature != null`,
// "" included) is a thinking block; otherwise a present redactedData is a
// redacted_thinking block.
func TestR77prova_AnthropicReasoningEmptySignatureIsThinking(t *testing.T) {
	for name, tc := range map[string]struct {
		opts     map[string]interface{}
		wantType string
	}{
		"empty signature":     {map[string]interface{}{"signature": ""}, "thinking"},
		"empty redacted data": {map[string]interface{}{"redactedData": ""}, "redacted_thinking"},
		"signature wins":      {map[string]interface{}{"signature": "", "redactedData": "x"}, "thinking"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := ConvertToAnthropicPrompt([]types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ReasoningContent{Text: "thought", ProviderOptions: map[string]interface{}{"anthropic": tc.opts}},
					types.TextContent{Text: "answer"},
				}},
			}, AnthropicPromptOptions{})
			if err != nil {
				t.Fatal(err)
			}
			content := out.Messages[1]["content"].([]map[string]interface{})
			if content[0]["type"] != tc.wantType {
				t.Fatalf("content = %v, warnings = %v", content, out.Warnings)
			}
			for _, w := range out.Warnings {
				if w.Message == "unsupported reasoning metadata" {
					t.Fatalf("unexpected warning %v", w)
				}
			}
		})
	}
}
