package prompt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR87genRemoveEmptyTextParts ports convert-to-language-model-prompt.test.ts
// "text parts" (user: "should filter out empty text parts", "should pass
// through non-empty text parts"; assistant: "should ignore empty text parts
// when there are no provider options", "should include empty text parts when
// there are provider options").
func TestR87genRemoveEmptyTextParts(t *testing.T) {
	call := types.ToolCallContent{ToolCallID: "toolCallId", ToolName: "toolName", Input: `{}`}
	opts := map[string]interface{}{"test-provider": map[string]interface{}{"key-a": "test-value-1"}}
	toolResult := types.ToolResultContent{ToolCallID: "toolCallId", ToolName: "toolName", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "r"}}
	in := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: ""}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello, world!"}, &types.TextContent{Text: "", ProviderOptions: opts}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: ""}, call}},
		{Role: types.RoleTool, Content: []types.ContentPart{toolResult}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "", ProviderOptions: opts}, call}},
		{Role: types.RoleTool, Content: []types.ContentPart{toolResult}},
	}
	before, _ := json.Marshal(in)
	got, err := NormalizePromptWithDownloadSupport(context.Background(), types.Prompt{Messages: in}, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Fatal("input messages were modified")
	}
	wantLens := []int{0, 1, 1, 1, 2, 1}
	if len(got.Messages) != len(wantLens) {
		t.Fatalf("got %d messages", len(got.Messages))
	}
	for i, m := range got.Messages {
		if len(m.Content) != wantLens[i] {
			t.Errorf("message %d: %d parts, want %d: %+v", i, len(m.Content), wantLens[i], m.Content)
		}
	}
	if tc, ok := got.Messages[1].Content[0].(types.TextContent); !ok || tc.Text != "hello, world!" {
		t.Errorf("message 1 kept %+v", got.Messages[1].Content)
	}
	if _, ok := got.Messages[2].Content[0].(types.ToolCallContent); !ok {
		t.Errorf("message 2 kept %+v", got.Messages[2].Content)
	}
	if tc, ok := got.Messages[4].Content[0].(types.TextContent); !ok || tc.Text != "" || tc.ProviderOptions == nil {
		t.Errorf("message 4 lost the empty text part with provider options: %+v", got.Messages[4].Content)
	}
}
