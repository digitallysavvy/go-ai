package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// convert-to-mistral-chat-messages.ts serializeMistralToolCallArguments (TS
// 4ae6986cd8): a string input that is valid JSON is sent as that text, text
// that is not JSON as a JSON string, other JSON as is.
func TestR85provaMistralToolCallArguments(t *testing.T) {
	for raw, want := range map[string]string{
		`"{\"b\":2,\"a\":1}"`: `{"b":2,"a":1}`,
		`{"a":1}`:             `{"a":1}`,
		`{"a":`:               `"{\"a\":"`,
		`"x"`:                 `"x"`,
	} {
		calls := mistralToolCalls([]types.ToolCall{{ID: "c", ToolName: "t", RawArguments: raw}})
		got := calls[0]["function"].(map[string]interface{})["arguments"]
		if got != want {
			t.Errorf("%s: arguments = %v, want %s", raw, got, want)
		}
	}
}
