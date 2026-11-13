package providerutils

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getErrorMessage(error) of a non-string, non-Error value is
// JSON.stringify(error): HTML characters stay literal and a non-finite
// number writes null (to-response-messages.ts createToolModelOutput,
// errorMode 'text').
func TestR98genToolErrorMessageJSONStringify(t *testing.T) {
	for name, tc := range map[string]struct {
		err  interface{}
		want string
	}{
		"html":      {map[string]interface{}{"html": "<b>&</b>"}, `{"html":"<b>&</b>"}`},
		"nonfinite": {map[string]interface{}{"x": math.Inf(1)}, `{"x":null}`},
	} {
		t.Run(name, func(t *testing.T) {
			msgs := ConvertToResponseMessages(nil, []types.ContentPart{
				types.ToolErrorContent{ToolCallID: "c1", ToolName: "t", Error: tc.err},
			}, nil)
			if len(msgs) != 1 || msgs[0].Role != types.RoleTool || len(msgs[0].Content) != 1 {
				t.Fatalf("messages = %#v", msgs)
			}
			part := msgs[0].Content[0].(types.ToolResultContent)
			if part.Output == nil || part.Output.Type != types.ToolResultOutputErrorText || part.Output.Value != tc.want {
				t.Fatalf("output = %#v, want error-text %s", part.Output, tc.want)
			}
		})
	}
}
