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
