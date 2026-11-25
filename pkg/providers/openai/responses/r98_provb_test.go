package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR98provbCallerArgsKeepPrototypeKeys: TS validates the caller's own
// tool args and tool-call input objects with validateTypes (no JSON parse,
// so no secureJsonParse check): a __proto__ / constructor key is an ordinary
// property. Go's validation re-parsed the marshaled value securely and
// failed with "forbidden prototype property".
func TestR98provbCallerArgsKeepPrototypeKeys(t *testing.T) {
	value := map[string]interface{}{"arguments": map[string]interface{}{"__proto__": map[string]interface{}{"x": 1}}, "call_id": "c"}
	if err := validateToolPayload(toolSearchInputNode, value); err != nil {
		t.Fatalf("validateToolPayload: %v", err)
	}
	if issues := zodIssueText(toolSearchInputNode, value); issues != "" {
		t.Fatalf("zodIssueText = %q", issues)
	}
	tool := types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: "openai.file_search", Name: "file_search",
		ProviderArgs: map[string]interface{}{"vectorStoreIds": []interface{}{"vs"}, "constructor": map[string]interface{}{"prototype": map[string]interface{}{}}}}
	if err := validateOpenAIToolArgsSchema(tool); err != nil {
		t.Fatalf("validateOpenAIToolArgsSchema: %v", err)
	}
}
