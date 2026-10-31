package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS stringifies object-form function.call arguments with JSON.stringify, so
// whitespace in the wire object never reaches the tool call input.
func TestR37_ConversationObjectArgumentsAreCompact(t *testing.T) {
	fnCall := `{"type":"function.call","tool_call_id":"function-1","name":"weather","arguments": { "city": "Madrid",  "n": 1 }}`
	_, p := newConvRecorder(t, "application/json", convOutputs(fnCall))
	res := generateWith(t, p, &provider.GenerateOptions{Prompt: whoWon()})
	fc := res.Content[0].(types.ToolCallContent)
	if fc.Input != `{"city":"Madrid","n":1}` {
		t.Errorf("content input = %q", fc.Input)
	}
	if res.ToolCalls[0].RawArguments != `{"city":"Madrid","n":1}` {
		t.Errorf("raw arguments = %q", res.ToolCalls[0].RawArguments)
	}
}
