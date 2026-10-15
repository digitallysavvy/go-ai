package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestToolResultContentOutputIsStringified(t *testing.T) {
	tr := types.ToolResultContent{ToolCallID: "a", Output: &types.ToolResultOutput{
		Type:    types.ToolResultOutputContent,
		Content: []types.ToolResultContentBlock{types.TextContentBlock{Text: "hi"}},
	}}
	if got := mistralToolResultText(tr); got != `[{"type":"text","text":"hi"}]` {
		t.Fatalf("content = %q", got)
	}
}
