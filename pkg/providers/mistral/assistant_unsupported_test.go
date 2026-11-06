package mistral

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestChatAssistantUnsupportedPartThrows(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.TextContent{Text: "hi"},
		types.CustomContent{Kind: "x"},
	}}}
	_, err := ConvertToMistralChatMessagesWithError(msgs)
	if err == nil || !strings.Contains(err.Error(), "Unsupported content type in assistant message: custom") {
		t.Fatalf("err = %v", err)
	}
}

// SDK-side parts (pointer parts, approval requests, tool errors, sources) are
// not TS prompt parts and must not trip the unsupported-part error.
func TestChatAssistantSDKSidePartsIgnored(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		&types.TextContent{Text: "hi"},
		types.ToolApprovalRequestContent{ApprovalID: "a"},
		types.ToolErrorContent{ToolCallID: "c"},
		types.SourceContent{SourceType: "url", URL: "https://x"},
		&types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: "{}"},
	}}}
	out, err := ConvertToMistralChatMessagesWithError(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if out[0]["content"] != "hi" || out[0]["tool_calls"] == nil {
		t.Fatalf("out = %#v", out[0])
	}
}
