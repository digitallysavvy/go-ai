package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r61: TS compaction matches an assistant message when any of its parts carries
// providerOptions.google.interactionId, tool-result and reasoning-file parts
// included.
func TestR61InteractionsCompactionMatchesEveryAssistantPartType(t *testing.T) {
	opts := map[string]interface{}{"google": map[string]interface{}{"interactionId": "v1_prev"}}
	for name, part := range map[string]types.ContentPart{
		"tool-result":    types.ToolResultContent{ToolCallID: "c1", ToolName: "google_search", ProviderExecuted: true, ProviderOptions: opts},
		"reasoning-file": types.ReasoningFileContent{MediaType: "image/png", Data: []byte{1}, ProviderOptions: opts},
	} {
		msgs := []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "first"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{part}},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "next"}}},
		}
		out := compactMessagesForInteraction(msgs, "v1_prev")
		if len(out) != 2 || out[0].Role != types.RoleUser || out[1].Role != types.RoleUser {
			t.Fatalf("%s: compacted messages = %#v, want only the two user messages", name, out)
		}
	}
}
