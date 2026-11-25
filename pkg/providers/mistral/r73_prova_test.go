package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prepareConversationTools drops every provider tool (tool.type ===
// 'provider') that is not a Mistral one with an unsupported warning, even one
// without an id or provider execution.
func TestR73prova_ConversationToolsDropOtherProviderTools(t *testing.T) {
	tools, _, warnings := prepareConversationTools([]types.Tool{
		{Name: "other", Type: types.ToolTypeProviderDefined},
		{Name: "fn", Parameters: map[string]interface{}{"type": "object"}},
	}, types.ToolChoice{})
	if len(tools) != 1 || tools[0]["type"] != "function" {
		t.Fatalf("tools = %v", tools)
	}
	if len(warnings) != 1 || warnings[0].Feature != "provider-defined tool other" {
		t.Fatalf("warnings = %v", warnings)
	}
}
