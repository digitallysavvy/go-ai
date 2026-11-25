package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS validateTypes(webFetch_20250910OutputSchema) parses content.citations as
// z.object({enabled}), so a replayed web fetch result sends only `enabled`
// (convert-to-anthropic-prompt.ts:1438-1453).
func TestR92provaAnthropicWebFetchCitationsStripped(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolCallContent{ToolCallID: "f", ToolName: "web_fetch", ProviderExecuted: true, Arguments: map[string]interface{}{"url": "https://x"}},
		types.ToolResultContent{ToolCallID: "f", ToolName: "web_fetch", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{
			"type": "web_fetch_result", "url": "https://x", "retrievedAt": nil,
			"content": map[string]interface{}{"type": "document", "title": nil,
				"citations": map[string]interface{}{"enabled": true, "extra": 1},
				"source":    map[string]interface{}{"type": "text", "mediaType": "text/plain", "data": "d"}},
		}}},
	}}}
	p, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "messages", p.Messages, `[{"role":"assistant","content":[
		{"type":"server_tool_use","id":"f","name":"web_fetch","input":{"url":"https://x"}},
		{"type":"web_fetch_tool_result","tool_use_id":"f","content":{"type":"web_fetch_result","url":"https://x","retrieved_at":null,
			"content":{"type":"document","title":null,"citations":{"enabled":true},"source":{"type":"text","media_type":"text/plain","data":"d"}}}}]}]`)
}
