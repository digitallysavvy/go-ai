package perplexity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports the perplexity-language-model.test.ts "tool choice" block (TS
// 7772d0ca0f): toolChoice none sends no tools (native MCP / connector
// configs included), max_tool_calls 0 (over a caller's value) and no
// unsupported warnings, for preset and direct model IDs.
func TestR88provgPerplexityToolChoiceNone(t *testing.T) {
	native := []interface{}{
		map[string]interface{}{"type": "web_search"},
		map[string]interface{}{"type": "mcp", "server_label": "test", "server_url": "https://example.com/mcp", "authorization": "test-mcp-token", "headers": map[string]interface{}{"x-api-key": "test-mcp-key"}},
		map[string]interface{}{"type": "connector", "id": "test-connector", "server_label": "test"},
	}
	for _, modelID := range []string{"low", "openai/gpt-5.1"} {
		for _, maxToolCalls := range []interface{}{nil, 5} {
			m := NewLanguageModel(New(Config{APIKey: "k"}), modelID)
			pplx := map[string]interface{}{"tools": native}
			if maxToolCalls != nil {
				pplx["max_tool_calls"] = maxToolCalls
			}
			body, warnings, err := m.getArgs(&provider.GenerateOptions{
				Prompt:          types.Prompt{Text: "hi"},
				Tools:           []types.Tool{{Name: "weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}, {Type: types.ToolTypeProviderDefined, Name: "search", ProviderID: "perplexity.web_search"}},
				ToolChoice:      types.ToolChoice{Type: types.ToolChoiceNone},
				ProviderOptions: map[string]interface{}{"perplexity": pplx},
			})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(body)
			if _, ok := body["tools"]; ok {
				t.Fatalf("%s: tools sent: %s", modelID, raw)
			}
			if body["max_tool_calls"] != 0 {
				t.Fatalf("%s/%v: max_tool_calls = %v", modelID, maxToolCalls, body["max_tool_calls"])
			}
			if strings.Contains(string(raw), "test-mcp-token") || strings.Contains(string(raw), "test-mcp-key") {
				t.Fatalf("native tool credentials sent: %s", raw)
			}
			if len(warnings) != 0 {
				t.Fatalf("warnings = %+v, want none", warnings)
			}
		}
	}
	// Without explicit tools (preset tools) max_tool_calls is still 0.
	body, _, err := NewLanguageModel(New(Config{APIKey: "k"}), "low").getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"}, ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone},
	})
	if err != nil || body["max_tool_calls"] != 0 {
		t.Fatalf("body = %v err = %v", body, err)
	}
}
