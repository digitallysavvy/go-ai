package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS deepseek prepareTools default branch: an unknown tool choice type sends
// no tool_choice and warns `tool choice type: <type>` (Go's shared
// ConvertToolChoiceToOpenAI would send "auto").
func TestR79provbDeepseekUnknownToolChoiceWarns(t *testing.T) {
	lm, err := New(Config{APIKey: "k"}).LanguageModel("deepseek-chat")
	if err != nil {
		t.Fatal(err)
	}
	tools := []types.Tool{{Name: "f", Parameters: map[string]interface{}{"type": "object"}}}
	dsTools, choice, warnings, err := lm.(*LanguageModel).prepareDeepSeekTools(tools, types.ToolChoice{Type: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dsTools) != 1 || choice != nil {
		t.Fatalf("tools %v, choice %#v", dsTools, choice)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "tool choice type: bogus" {
		t.Fatalf("warnings = %+v", warnings)
	}
	_, choice, warnings, _ = lm.(*LanguageModel).prepareDeepSeekTools(tools, types.ToolChoice{Type: types.ToolChoiceRequired})
	if choice != "required" || len(warnings) != 0 {
		t.Fatalf("required: %#v %v", choice, warnings)
	}
}
