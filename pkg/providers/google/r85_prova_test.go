package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// google-interactions-language-model.test.ts "omits $name when toolChoice is
// none" (TS a32df11498): toolChoice none omits provider-executed tools too,
// with no tool_choice and no tool warnings.
func TestR85provaInteractionsToolChoiceNone(t *testing.T) {
	providerTools := []types.Tool{
		{Type: types.ToolTypeProviderDefined, ProviderID: "google.google_search", Name: "search", ProviderArgs: map[string]interface{}{}},
		{Type: types.ToolTypeProviderDefined, ProviderID: "google.code_execution", Name: "code", ProviderArgs: map[string]interface{}{}},
		{Type: types.ToolTypeProviderDefined, ProviderID: "google.mcp_server", Name: "connector", ProviderArgs: map[string]interface{}{
			"name": "my-mcp", "url": "https://example.com/mcp",
			"headers": map[string]interface{}{"Authorization": "Bearer test-connector-token"}, "allowedTools": []interface{}{"getWeather"},
		}},
		{Type: "unsupported-kind", Name: "dropped"},
	}
	withFunction := append(append([]types.Tool{}, providerTools...), types.Tool{Type: types.ToolTypeFunction, Name: "getWeather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}})
	for name, tools := range map[string][]types.Tool{"provider tools": providerTools, "function and provider tools": withFunction} {
		out, choice, warnings := prepareInteractionsTools(tools, types.ToolChoice{Type: types.ToolChoiceNone})
		if out != nil || choice != nil || len(warnings) != 0 {
			t.Errorf("%s: tools = %v, choice = %v, warnings = %v", name, out, choice, warnings)
		}
	}
}
