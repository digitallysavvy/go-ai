package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Pointer content parts convert like their value form instead of being dropped.
func TestConvertMistralPointerPartsAreNotDropped(t *testing.T) {
	out, err := ConvertToMistralChatMessagesWithError([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{&types.TextContent{Text: "hi"}}},
		{Role: types.RoleTool, Content: []types.ContentPart{&types.ToolResultContent{
			ToolCallID: "c1", ToolName: "t",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("messages = %+v", out)
	}
	if parts, _ := out[0]["content"].([]map[string]interface{}); len(parts) != 1 || parts[0]["text"] != "hi" {
		t.Fatalf("user = %+v", out[0])
	}
	if out[1]["tool_call_id"] != "c1" || out[1]["content"] != "ok" {
		t.Fatalf("tool = %+v", out[1])
	}
}
