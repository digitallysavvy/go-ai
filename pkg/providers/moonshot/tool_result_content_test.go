package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A natively built content output carries its blocks in Content (Value is
// nil); TS JSON.stringify(output.value) is the block array.
func TestToolResultContentOutputIsStringified(t *testing.T) {
	tr := types.ToolResultContent{ToolCallID: "a", Output: &types.ToolResultOutput{
		Type:    types.ToolResultOutputContent,
		Content: []types.ToolResultContentBlock{types.TextContentBlock{Text: "hi"}},
	}}
	if got := extractMoonshotToolResultText(tr); got != `[{"type":"text","text":"hi"}]` {
		t.Fatalf("content = %q", got)
	}
}
