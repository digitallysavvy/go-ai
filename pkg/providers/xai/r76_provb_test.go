package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR76ProvbFunctionToolWithoutDescription: TS xAI prepareResponsesTools
// sends `description: tool.description`, so a function tool without a
// description has no description key on the wire; Go sent "".
func TestR76ProvbFunctionToolWithoutDescription(t *testing.T) {
	tools := prepareXAIResponsesTools([]types.Tool{
		{Name: "f", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "g", Description: "does g", Parameters: map[string]interface{}{"type": "object"}},
	})
	f, _ := tools[0].(map[string]interface{})
	if _, ok := f["description"]; ok {
		t.Errorf("tool f = %v, want no description key", f)
	}
	g, _ := tools[1].(map[string]interface{})
	if g["description"] != "does g" {
		t.Errorf("tool g = %v", g)
	}
}
