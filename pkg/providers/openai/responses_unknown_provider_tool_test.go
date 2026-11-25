package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TS openai-responses-prepare-tools: a `provider` tool with an id the
// Responses API does not know is dropped silently (the inner switch on
// tool.id has no default); only a tool that is neither function nor provider
// reaches the `function tool` warning.
func TestResponsesUnknownProviderToolIsDroppedWithoutWarning(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewResponsesLanguageModel(p, "gpt-4o")
	tools := []types.Tool{
		{Name: "x", Type: "provider", ProviderID: "acme.search"},
		{Name: "lookup", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
	}
	if got := responses.PrepareTools(tools); len(got) != 1 {
		t.Fatalf("prepared tools = %v", got)
	}
	_, _, warnings, err := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		Tools:  tools,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warnings {
		if w.Type == "unsupported" {
			t.Fatalf("unexpected unsupported warning: %+v", w)
		}
	}
}

// TS lists programmatic_tool_calling among the provider tool names a
// toolChoice of type 'tool' maps to {type: <name>}.
func TestResponsesToolChoiceProgrammaticToolCalling(t *testing.T) {
	tools := []types.Tool{{Name: "openai.programmatic_tool_calling", ProviderID: "openai.programmatic_tool_calling", Type: "provider"}}
	got := convertResponsesToolChoice(types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "openai.programmatic_tool_calling"}, tools)
	m, ok := got.(map[string]interface{})
	if !ok || m["type"] != "programmatic_tool_calling" || len(m) != 1 {
		t.Fatalf("tool choice = %#v", got)
	}
}
