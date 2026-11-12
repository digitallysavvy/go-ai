package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The function_call namespace and async are read from the part's provider
// data through its JSON form, like its itemId (TS
// part.providerOptions?.[name]?.namespace): a Go-typed literal was ignored.
func TestR92provbFunctionCallNamespaceTypedLiteral(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleAssistant,
		Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: `{}`,
			ProviderOptions: map[string]interface{}{"openai": map[string]string{"namespace": "ns1"}}}},
		ToolCalls: []types.ToolCall{{ID: "c2", ToolName: "g", RawArguments: `{}`,
			ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"namespace": "ns2", "async": true}}},
			{ID: "c3", ToolName: "h", RawArguments: `{}`,
				ProviderMetadata: map[string]interface{}{"openai": map[string]string{"namespace": "ns3"}}}},
	}}}
	input, _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(input)
	for _, want := range []string{`"call_id":"c1"`, `"namespace":"ns1"`, `"namespace":"ns2"`, `"namespace":"ns3"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("input %s lacks %s", b, want)
		}
	}
}
