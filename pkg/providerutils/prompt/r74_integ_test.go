package prompt

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getAnthropicToolsetNames selects toolset tools by id and keys the
// result by the caller's tool name; Go constructors name tools after the id.
func TestR74Integ_AnthropicToolsetNamesByID(t *testing.T) {
	got := AnthropicToolsetNames([]types.Tool{
		{Name: "anthropic.computer_toolset_20260801"},
		{Name: "desktop", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.computer_toolset_20260801"},
		{Name: "anthropic.bash_20250124"},
		{Name: "other", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.bash_20250124"},
	})
	want := map[string]string{"anthropic.computer_toolset_20260801": "computer", "desktop": "computer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("toolset names = %v, want %v", got, want)
	}
}
