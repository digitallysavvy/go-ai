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
