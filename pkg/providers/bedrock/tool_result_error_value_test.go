package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The Go-only generic error output carries an arbitrary value; a non-string
// value must be JSON-stringified, not rendered with Go's %v ("map[code:7]").
func TestBedrockToolResultGenericErrorNonStringValueIsJSON(t *testing.T) {
	out, err := bedrockToolResultContent(types.ToolResultContent{
		ToolCallID: "c", ToolName: "t",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputError, Value: map[string]interface{}{"code": 7}},
	}, func(string) string { return "doc" })
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0]["text"] != `{"code":7}` {
		t.Fatalf("out = %#v", out)
	}
}
