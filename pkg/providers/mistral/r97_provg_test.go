package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertToMistralConversationInputs reads part.input.arguments with an
// exact key: an "Arguments" key is not it, so the whole input is serialized
// as the function.call arguments.
func TestR97provgConversationExecutionArgumentsKeyExact(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
		ToolCallID: "c", ToolName: "web_search", Input: `{"Arguments":"x"}`, ProviderExecuted: true,
	}}}}
	inputs, _, err := convertToMistralConversationInputs(msgs, conversationToolNames{})
	if err != nil {
		t.Fatal(err)
	}
	if got := inputs[0]["arguments"]; got == "x" {
		t.Fatalf("arguments = %v, want the serialized input", got)
	}

	msgs[0].Content[0] = types.ToolCallContent{ToolCallID: "c", ToolName: "web_search", Input: `{"arguments":"x"}`, ProviderExecuted: true}
	inputs, _, err = convertToMistralConversationInputs(msgs, conversationToolNames{})
	if err != nil {
		t.Fatal(err)
	}
	if got := inputs[0]["arguments"]; got != "x" {
		t.Fatalf("arguments = %v, want x", got)
	}
}
