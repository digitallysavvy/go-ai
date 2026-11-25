package bedrock

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS JSON.stringify never escapes <, > or &; the model must see them verbatim.
func TestToolResultJSONIsNotHTMLEscaped(t *testing.T) {
	out, err := bedrockToolResultContent(types.ToolResultContent{
		ToolCallID: "c", ToolName: "t",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"html": "<b>&</b>"}},
	}, func(string) string { return "doc" })
	if err != nil {
		t.Fatal(err)
	}
	text, _ := out[0]["text"].(string)
	if text != `{"html":"<b>&</b>"}` || strings.Contains(text, "\\u003c") {
		t.Fatalf("tool result text = %q", text)
	}
}
