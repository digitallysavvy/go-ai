package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestIsUndeclaredParallelToolCall_CollisionScopedToFunctionTools covers row
// 6be0f51: TS's isUndeclaredParallelToolCall checks the collision only
// against `functionTools` (tools.filter(t => t.type === 'function')), not
// the full request tool list. A provider-defined/built-in tool literally
// named "parallel" must not suppress wrapper expansion; only a real
// client-side function tool named "parallel" does.
func TestIsUndeclaredParallelToolCall_CollisionScopedToFunctionTools(t *testing.T) {
	t.Run("provider-defined tool named parallel does not suppress expansion", func(t *testing.T) {
		tools := []types.Tool{
			{Name: "parallel", Type: types.ToolTypeProviderDefined, ProviderID: "openai.web_search"},
			{Name: "get_weather", Parameters: map[string]interface{}{"type": "object"}},
		}
		if !IsUndeclaredParallelToolCall("parallel", tools) {
			t.Fatal("want the wrapper to still be treated as undeclared/expandable")
		}
	})

	t.Run("provider-executed tool named parallel does not suppress expansion", func(t *testing.T) {
		tools := []types.Tool{
			{Name: "parallel", ProviderExecuted: true},
		}
		if !IsUndeclaredParallelToolCall("parallel", tools) {
			t.Fatal("want the wrapper to still be treated as undeclared/expandable")
		}
	})

	t.Run("a real function tool named parallel suppresses expansion", func(t *testing.T) {
		tools := []types.Tool{
			{Name: "parallel", Parameters: map[string]interface{}{"type": "object"}},
		}
		if IsUndeclaredParallelToolCall("parallel", tools) {
			t.Fatal("want the user's own \"parallel\" function tool to win, not the wrapper")
		}
	})
}
