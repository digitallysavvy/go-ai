package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prepareTools: a tool with type 'provider' is dropped with an
// `unsupported` warning `provider-defined tool <id>`, never sent as a function.
func TestPrepareMoonshotToolsDropsProviderTypedTools(t *testing.T) {
	tools, _, warnings, err := prepareMoonshotTools([]types.Tool{
		{Name: "search", Type: "provider", ProviderID: "acme.search"},
		{Name: "lookup", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
	}, types.ToolChoice{}, false, "kimi-k2.5")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Function.Name != "lookup" {
		t.Fatalf("tools = %+v", tools)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "provider-defined tool acme.search" {
		t.Fatalf("warnings = %+v", warnings)
	}
}
