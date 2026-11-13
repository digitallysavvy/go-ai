package prompt

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS sends tool_name: anthropicOptions.toolName! for a tool-reference
// custom content part; an absent toolName is undefined and JSON.stringify
// leaves the key out (Go sent "tool_name": null).
func TestR99provaToolReferenceWithoutToolName(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "search", Arguments: map[string]interface{}{}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "c1", ToolName: "search",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
				types.CustomContentBlock{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"type": "tool-reference"}}},
			}},
		}}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out.Messages[1]["content"])
	want := `[{"content":[{"type":"tool_reference"}],"tool_use_id":"c1","type":"tool_result"}]`
	if string(b) != want {
		t.Fatalf("content = %s, want %s", b, want)
	}
}
