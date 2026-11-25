package prompt

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// core2-14: a user message with no parts sends content: [] like TS
// (convert-to-openai-compatible-chat-messages.ts:255-262,
// convert-to-openai-chat-messages.ts:144-147).
func TestR91provbEmptyUserContent(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{}}}
	for name, opt := range map[string]ToOpenAIMessagesOptions{
		"openai":     {},
		"compatible": {FileParts: FilePartsOpenAICompatible, SpreadCompatMetadata: true},
	} {
		b, _ := json.Marshal(ToOpenAIMessages(msgs, opt))
		if string(b) != `[{"content":[],"role":"user"}]` {
			t.Fatalf("%s: %s", name, b)
		}
	}
}
