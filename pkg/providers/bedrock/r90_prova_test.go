package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 84208104a4 amazon-bedrock-prepare-tools.test.ts: "should warn when strict
// is omitted for %s" covers the Fable 5.2 IDs.
func TestR90provaFable52StrictWarning(t *testing.T) {
	strict := true
	tools := []types.Tool{{Type: types.ToolTypeFunction, Name: "testFunction", Description: "A test function", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, Strict: &strict}}
	for _, id := range []string{ModelAnthropicClaudeFable5_2, ModelUSAnthropicClaudeFable5_2, ModelGlobalAnthropicClaudeFable5_2} {
		res := prepareBedrockTools(tools, types.ToolChoice{}, false, id, "", nil, nil, false)
		spec, _ := res.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})
		if _, ok := spec["strict"]; ok {
			t.Errorf("%s: toolSpec.strict present", id)
		}
		want := "Tool 'testFunction' has strict: true, but strict mode is not supported by this model on Amazon Bedrock. The strict property will be ignored."
		if len(res.Warnings) != 1 || res.Warnings[0].Type != "unsupported" || res.Warnings[0].Feature != "strict" || res.Warnings[0].Details != want {
			t.Errorf("%s: warnings = %#v", id, res.Warnings)
		}
	}
}
