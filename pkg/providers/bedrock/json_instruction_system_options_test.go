package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS injectJsonInstructionIntoMessages spreads the existing system message, so
// message-level providerOptions (a cachePoint) survive the injection.
func TestInjectBedrockJSONInstructionKeepsSystemProviderOptions(t *testing.T) {
	msgs := []types.Message{{
		Role:            types.RoleSystem,
		Content:         []types.ContentPart{types.TextContent{Text: "be brief"}},
		ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"cachePoint": true}},
	}, {Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}

	out := injectBedrockJSONInstruction(msgs, map[string]interface{}{"type": "object"})
	system, _, err := convertToBedrockChatMessages(out, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(system) != 2 || system[1]["cachePoint"] == nil {
		t.Fatalf("system = %#v, want text block followed by cachePoint", system)
	}
}
