package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertToOpenAIResponsesInput warns "system messages are removed for this
// model" for every system message in remove mode, including the system prompt.
func TestR37_SystemPromptRemovedWarns(t *testing.T) {
	prompt := types.Prompt{System: "be brief", Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}}
	input, warnings, err := ConvertPromptToInputWithOptions(prompt, "remove", ConvertOptions{Store: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 1 {
		t.Errorf("input = %#v", input)
	}
	if len(warnings) != 1 || warnings[0].Type != "other" || warnings[0].Message != "system messages are removed for this model" {
		t.Errorf("warnings = %#v", warnings)
	}
	// No system prompt, no warning.
	prompt.System = ""
	_, warnings, _ = ConvertPromptToInputWithOptions(prompt, "remove", ConvertOptions{Store: true})
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings %#v", warnings)
	}
}
