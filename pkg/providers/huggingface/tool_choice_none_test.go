package huggingface

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23843: toolChoice none is forwarded to the Responses API.
func TestPrepareResponsesToolsForwardsToolChoiceNone(t *testing.T) {
	tools := []types.Tool{{Name: "weather", Description: "d", Parameters: map[string]interface{}{"type": "object"}}}
	_, _, mapped, _ := prepareResponsesTools(tools, types.ToolChoice{Type: types.ToolChoiceNone})
	if mapped != "none" {
		t.Fatalf("tool_choice = %#v, want none", mapped)
	}
	_, present, _, _ := prepareResponsesTools(tools, types.ToolChoice{Type: types.ToolChoiceNone})
	if !present {
		t.Fatal("tool definitions must be preserved with tool choice none")
	}
}
