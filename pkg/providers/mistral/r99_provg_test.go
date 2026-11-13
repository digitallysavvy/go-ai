package mistral

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS reads part.providerOptions (the replayed providerMetadata object) as is:
// a number beyond the double range is Infinity there (ruling 4) and the
// mistral name still applies. encoding/json failed the whole decode, so the
// built-in call lost its wire name (cr-r99-gen-to-provg).
func TestR99provgConversationReplayMetadataOutOfRange(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
		ToolCallID: "c1", ToolName: "web_search", Input: `{"query":"q"}`, ProviderExecuted: true,
		ProviderMetadata: json.RawMessage(`{"mistral":{"name":"web_search_premium","score":1e400}}`),
	}}}}
	inputs, _, err := convertToMistralConversationInputs(msgs, newConversationToolNames(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 || inputs[0]["name"] != "web_search_premium" {
		t.Fatalf("inputs = %#v, want the metadata wire name", inputs)
	}
}
