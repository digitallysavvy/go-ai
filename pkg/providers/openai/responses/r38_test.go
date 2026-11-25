package responses

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// webSearchOutputSchema's source members are z.object, which strips unknown
// keys, so only type and url/name are replayed.
func TestR38_WebSearchSourcesAreStrippedToSchemaMembers(t *testing.T) {
	part := types.ToolResultContent{
		ToolCallID: "ws_1",
		ToolName:   "web_search",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{
			"action": map[string]interface{}{"type": "search", "query": "q"},
			"sources": []interface{}{
				map[string]interface{}{"type": "url", "url": "https://a.example", "title": "A"},
				map[string]interface{}{"type": "api", "name": "n", "extra": 1},
			},
		}},
	}
	got, _ := convertWebSearchToolResultOutput(part, "ws_1").(map[string]interface{})
	action, _ := got["action"].(map[string]interface{})
	want := []interface{}{
		map[string]interface{}{"type": "url", "url": "https://a.example"},
		map[string]interface{}{"type": "api", "name": "n"},
	}
	if !reflect.DeepEqual(action["sources"], want) {
		t.Fatalf("sources = %#v", action["sources"])
	}
}
