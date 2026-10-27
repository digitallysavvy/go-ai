package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// Ported from azure-openai-provider.test.ts "tool search tool" (TS #24066):
// function tool options are read from providerOptions[<provider name>] with
// providerOptions.openai as the fallback.
func TestResponsesFunctionToolOptionsUseProviderOptionsName(t *testing.T) {
	for _, tc := range []struct {
		name, providerName, optionsKey string
		wantDefer                      bool
	}{
		{"azure on azure", "azure", "azure", true},
		{"openai fallback on azure", "azure", "openai", true},
		{"azure key ignored on openai", "", "azure", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := New(Config{APIKey: "k", ResponsesProviderOptionsName: tc.providerName})
			model := NewResponsesLanguageModel(p, "gpt-5.4")
			opts := map[string]interface{}{"deferLoading": true}
			tools := []types.Tool{
				openaitool.ToolSearch(openaitool.ToolSearchArgs{}),
				{
					Type:            types.ToolTypeFunction,
					Name:            "get_weather",
					Parameters:      map[string]interface{}{"type": "object"},
					ProviderOptions: map[string]interface{}{tc.optionsKey: opts},
				},
			}
			body, _, err := model.buildRequestBody(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				Tools:  tools,
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			prepared, ok := body["tools"].([]interface{})
			if !ok || len(prepared) != 2 {
				t.Fatalf("tools = %#v", body["tools"])
			}
			fn, ok := prepared[1].(responses.FunctionToolDef)
			if !ok {
				t.Fatalf("tool[1] = %#v", prepared[1])
			}
			if got := fn.DeferLoading != nil && *fn.DeferLoading; got != tc.wantDefer {
				t.Fatalf("defer_loading = %v, want %v", got, tc.wantDefer)
			}
			// Caller's tool options are not mutated.
			if _, has := tools[1].ProviderOptions.(map[string]interface{})["openai"]; has != (tc.optionsKey == "openai") {
				t.Fatalf("caller options mutated: %#v", tools[1].ProviderOptions)
			}
		})
	}
}
