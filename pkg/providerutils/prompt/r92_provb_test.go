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
