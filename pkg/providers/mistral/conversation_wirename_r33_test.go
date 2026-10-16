package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS `mistral?.function ?? mistral?.name`: a non-nullish non-string `function`
// shadows a string `name`, so the mapped provider tool name is used.
func TestConversationWireNameFunctionShadowsNameR33(t *testing.T) {
	names := conversationToolNames{}
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
		ToolCallID: "c", ToolName: "web_search", Input: `{}`, ProviderExecuted: true,
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"function": 5, "name": "from_name"}},
	}}}}
	inputs, _, err := convertToMistralConversationInputs(msgs, names)
	if err != nil {
		t.Fatal(err)
	}
	if got := inputs[0]["name"]; got == "from_name" {
		t.Fatalf("name = %v, want the mapped provider tool name", got)
	}
}
