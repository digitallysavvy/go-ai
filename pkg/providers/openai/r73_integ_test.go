package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// r73 integrator survivor test (provb fix 7): a TS-shaped openai.custom tool
// (Type provider, ProviderID, ProviderArgs, e.g. restored from a workflow,
// where ProviderOptions is no longer a CustomTool) is still a custom tool for
// the Responses tool choice: TS sends {type: "custom", name}.
func TestR73IntegTSShapedCustomToolChoice(t *testing.T) {
	syntax, definition := "regex", "a+"
	typed := openaitool.NewCustomTool(openaitool.WithFormat(openaitool.CustomToolFormat{Type: "grammar", Syntax: &syntax, Definition: &definition})).ToTool("re")
	tsShaped := types.Tool{Name: typed.Name, Type: types.ToolTypeProviderDefined, ProviderID: typed.ProviderID, ProviderArgs: typed.ProviderArgs,
		ProviderOptions: map[string]interface{}{"not": "typed"}}
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5")
	for _, tool := range []types.Tool{typed, tsShaped} {
		body, _, _, err := model.buildRequest(&provider.GenerateOptions{
			Prompt:     types.Prompt{Text: "hi"},
			Tools:      []types.Tool{tool},
			ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "re"},
		}, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := r73provbJSON(t, body["tool_choice"]); got != `{"name":"re","type":"custom"}` {
			t.Errorf("options %T: tool_choice = %s, want custom", tool.ProviderOptions, got)
		}
	}
}
