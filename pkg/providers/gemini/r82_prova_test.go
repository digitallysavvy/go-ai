package gemini

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS doStream tool-result for a server toolResponse carries providerMetadata
// {google: {serverToolCallId, serverToolType}}; core reads the result's own
// metadata (stream content, UI tool-output part), and Go set it only on the
// chunk (TS wire replay "should stream server-side toolCall and toolResponse
// parts (tool combination)").
func TestR82provaStreamServerToolResultMetadata(t *testing.T) {
	s := newTestStream(sseStream(`{"candidates":[{"content":{"parts":[{"toolCall":{"toolType":"GOOGLE_SEARCH_WEB","id":"sc-1","args":{}}},{"toolResponse":{"toolType":"GOOGLE_SEARCH_WEB","id":"sc-1","response":{"ok":true}}}]}}]}`))
	want := map[string]interface{}{"google": map[string]interface{}{"serverToolCallId": "sc-1", "serverToolType": "GOOGLE_SEARCH_WEB"}}
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal("no tool-result chunk")
		}
		if c.Type == provider.ChunkTypeToolResult {
			if c.ToolResult == nil || !reflect.DeepEqual(c.ToolResult.ProviderMetadata, want) {
				t.Fatalf("tool result metadata = %#v", c.ToolResult)
			}
			return
		}
	}
}
