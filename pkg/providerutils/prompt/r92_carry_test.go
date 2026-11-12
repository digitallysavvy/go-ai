package prompt

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS merge-objects.ts skips only undefined override values: null replaces
// the base value (as in the default-settings middleware).
func TestR92carryMergeProviderOptionsNullOverrides(t *testing.T) {
	base := map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "high", "store": true}}
	overrides := map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": nil}}
	got := MergeProviderOptions(base, overrides)
	want := map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": nil, "store": true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge = %#v, want %#v", got, want)
	}
	if got := MergeProviderOptions(map[string]interface{}{"a": 1}, map[string]interface{}{"a": nil}); !reflect.DeepEqual(got, map[string]interface{}{"a": nil}) {
		t.Fatalf("top-level null = %#v", got)
	}
	// The tool-message push-down merges the same way: a part's null wins.
	msgs := MergeConsecutiveToolMessages([]types.Message{
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "a", ProviderOptions: map[string]interface{}{"x": nil}}}, ProviderOptions: map[string]interface{}{"x": 1}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "b"}}},
	})
	part := msgs[0].Content[0].(types.ToolResultContent)
	if v, ok := part.ProviderOptions["x"]; !ok || v != nil {
		t.Fatalf("part options = %#v", part.ProviderOptions)
	}
}
