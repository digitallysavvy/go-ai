package prompt

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR96provgGoogleToolCallInputPrototypeKey: TS parseToolCallInput uses
// secureJsonParse, so a string input with a __proto__ key (or a
// constructor.prototype) is sent as the raw string.
func TestR96provgGoogleToolCallInputPrototypeKey(t *testing.T) {
	const input = `{"__proto__":{"x":1}}`
	for name, msg := range map[string]types.Message{
		"part":     {Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c", ToolName: "f", Input: input}}},
		"toolCall": {Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "c", ToolName: "f", RawArguments: input}}},
	} {
		out, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}, msg}, GoogleMessagesOptions{IncludeFunctionCallIDs: true})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := json.Marshal(out.Contents[1]["parts"])
		const want = `[{"functionCall":{"args":"{\"__proto__\":{\"x\":1}}","id":"c","name":"f"}}]`
		if string(b) != want {
			t.Errorf("%s: parts = %s, want %s", name, b, want)
		}
	}
}
