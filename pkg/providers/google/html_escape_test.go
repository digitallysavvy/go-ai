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

// TS convertToolResultPart: error-json is JSON.stringify(output.value), not Go's
// %v rendering of a map.
func TestInteractionsToolResultErrorJSONIsStringified(t *testing.T) {
	block, _, err := toolResultToInteractionBlock(types.ToolResultContent{
		ToolCallID: "c", ToolName: "t",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON, Value: map[string]interface{}{"code": 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if block["result"] != `{"code":7}` || block["is_error"] != true {
		t.Fatalf("block = %#v", block)
	}
}

// TS keys the image-only check on the top-level media type, so a bare "image"
// tool-result file is kept.
func TestInteractionsToolResultBareImageMediaTypeKept(t *testing.T) {
	block, warnings, err := toolResultToInteractionBlock(types.ToolResultContent{
		ToolCallID: "c", ToolName: "t",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{MediaType: "image", URL: "https://x/y.png"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := block["result"].([]map[string]interface{})
	if len(warnings) != 0 || len(blocks) != 1 || blocks[0]["type"] != "image" {
		t.Fatalf("warnings=%v blocks=%#v", warnings, blocks)
	}
}
