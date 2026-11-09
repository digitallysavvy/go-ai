package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS perplexity-prepare-tools.ts: description: tool.description, so a tool
// without a description sends no description key (Go "" = unset).
func TestR89provgPerplexityToolWithoutDescription(t *testing.T) {
	tools, _ := preparePerplexityTools([]types.Tool{
		{Type: types.ToolTypeFunction, Name: "a", Parameters: map[string]interface{}{"type": "object"}},
		{Type: types.ToolTypeFunction, Name: "b", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
	}, types.ToolChoice{})
	if _, ok := tools[0]["description"]; ok {
		t.Fatalf("tool without description sent %#v", tools[0])
	}
	if tools[1]["description"] != "d" {
		t.Fatalf("tool description = %#v", tools[1])
	}
}
