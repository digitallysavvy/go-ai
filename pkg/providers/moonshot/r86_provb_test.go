package moonshot

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86provbMessageToolsGoTypedList: a system message's dynamic tools given
// as []map[string]interface{} convert like the JSON form (TS parses the
// array); the call failed with "invalid moonshotai provider options".
func TestR86provbMessageToolsGoTypedList(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}}
	convert := func(tools interface{}) string {
		msgs := []types.Message{
			moonshotMsg(types.RoleSystem, map[string]interface{}{"tools": tools}),
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}
		out, _, err := convertToMoonshotChatMessages("kimi-k3", msgs, "")
		if err != nil {
			t.Fatalf("convert: %v", err)
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	got := convert([]map[string]interface{}{{"type": "function", "name": "calc", "description": "d", "inputSchema": schema}})
	want := convert([]interface{}{map[string]interface{}{"type": "function", "name": "calc", "description": "d", "inputSchema": schema}})
	if got != want {
		t.Fatalf("typed = %s\njson  = %s", got, want)
	}
}
