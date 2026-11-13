package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS system messages carry one content string: several system text parts
// are joined, and an empty system message is content "".
func TestR92provbSystemMessageJoinedText(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "a"}, types.TextContent{Text: "b"}}},
		{Role: types.RoleSystem},
	}
	out := ToOpenAIMessages(msgs, ToOpenAIMessagesOptions{SpreadCompatMetadata: true})
	if len(out) != 2 || out[0]["content"] != "ab" || out[1]["content"] != "" {
		t.Fatalf("messages = %#v, want contents \"ab\" and \"\"", out)
	}
}

// TS convertToolContentPart spreads each content part's
// providerOptions.openaiCompatible metadata into the multi-part tool content.
func TestR92provbMultiPartToolContentMetadata(t *testing.T) {
	meta := map[string]interface{}{"openaiCompatible": map[string]interface{}{"cache": "x"}}
	msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
		ToolCallID: "c", ToolName: "t",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.TextContentBlock{Text: "hi", ProviderOptions: meta},
		}},
	}}}}
	out := ToOpenAIMessages(msgs, ToOpenAIMessagesOptions{SupportsMultiPartToolContent: true})
	parts, ok := out[0]["content"].([]map[string]interface{})
	if !ok || len(parts) != 1 || parts[0]["cache"] != "x" {
		t.Fatalf("tool content = %#v, want the text part with cache: x", out[0]["content"])
	}
}
