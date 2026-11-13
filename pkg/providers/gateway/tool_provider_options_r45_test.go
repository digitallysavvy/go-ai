package gateway

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prepareTools copies a function tool's providerOptions onto the
// LanguageModelV4FunctionTool sent to the gateway.
func TestGatewayLanguageModelSendsFunctionToolProviderOptions(t *testing.T) {
	model := newTestGatewayLanguageModel("anthropic/claude-sonnet-4-6")
	opts := map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		Tools: []types.Tool{
			{Name: "a", Parameters: map[string]interface{}{"type": "object"}, ProviderOptions: opts},
			{Name: "b", Parameters: map[string]interface{}{"type": "object"}},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]map[string]interface{})
	got, _ := tools[0]["providerOptions"].(map[string]interface{})
	if got["anthropic"] == nil {
		t.Fatalf("tool a = %#v", tools[0])
	}
	if _, ok := tools[1]["providerOptions"]; ok {
		t.Fatalf("tool b = %#v", tools[1])
	}
}
