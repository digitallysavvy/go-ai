package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS webSearchOutputSchema: query/queries/sources are .optional() (null is a
// validation failure, so the result is not replayed as a web_search_call);
// openPage url is .nullish() (null is kept).
func TestConvertWebSearchToolResultOutputNullMembers(t *testing.T) {
	result := func(v map[string]interface{}) types.ToolResultContent {
		return types.ToolResultContent{ToolCallID: "ws_1", ToolName: "web_search", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: v}}
	}
	for name, v := range map[string]map[string]interface{}{
		"query null":   {"action": map[string]interface{}{"type": "search", "query": nil}},
		"queries null": {"action": map[string]interface{}{"type": "search", "queries": nil}},
		"sources null": {"action": map[string]interface{}{"type": "search", "query": "q"}, "sources": nil},
	} {
		if got := convertWebSearchToolResultOutput(result(v), "ws_1"); got != nil {
			t.Errorf("%s: converted to %#v, want nil (schema validation fails)", name, got)
		}
	}
	ok := convertWebSearchToolResultOutput(result(map[string]interface{}{"action": map[string]interface{}{"type": "openPage", "url": nil}}), "ws_1")
	m, isMap := ok.(map[string]interface{})
	if !isMap {
		t.Fatalf("openPage with null url not converted: %#v", ok)
	}
	if a := m["action"].(map[string]interface{}); a["type"] != "open_page" {
		t.Errorf("action = %#v", a)
	}
	if got := convertWebSearchToolResultOutput(result(map[string]interface{}{"action": map[string]interface{}{"type": "search", "query": "q"}}), "ws_1"); got == nil {
		t.Errorf("plain search must still convert")
	}
}
