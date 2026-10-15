package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestInteractionsToolResultJSONIsNotHTMLEscaped(t *testing.T) {
	block, _, err := toolResultToInteractionBlock(types.ToolResultContent{
		ToolCallID: "c", ToolName: "t",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"html": "<b>&</b>"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if block["result"] != `{"html":"<b>&</b>"}` {
		t.Fatalf("result = %q", block["result"])
	}
}

func TestIsFullMediaType(t *testing.T) {
	for in, want := range map[string]bool{"image/png": true, "image/*": false, "image/": false, "image": false, "": false, "application/pdf": true} {
		if got := isFullMediaType(in); got != want {
			t.Errorf("isFullMediaType(%q) = %v, want %v", in, got, want)
		}
	}
}
