package responses

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS: `OpenAI does not support ${tool.type} tools in tool_choice.allowed_tools`
// uses the wire tool type, not the provider id.
func TestResolveAllowedToolsUnsupportedReasonUsesWireToolType(t *testing.T) {
	tools := []types.Tool{
		{Name: "openai.tool_search", ProviderID: "openai.tool_search", Type: types.ToolTypeProviderDefined},
		{Name: "get", Description: "d"},
	}
	_, warnings, err := ResolveAllowedTools(tools, []string{"openai.tool_search", "get"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Details, "OpenAI does not support tool_search tools in tool_choice.allowed_tools;") {
		t.Fatalf("warnings = %#v", warnings)
	}
}
