package tsfixture

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR75ProvbPromptPartProviderMetadata: a TS prompt's tool-call and
// tool-result parts keep the providerMetadata the TS tests cast onto them
// (convert-to-openai-responses-input-tool-search.test.ts reads
// providerMetadata.openai.itemId from both); before r75 the tool-result
// part dropped it.
func TestR75ProvbPromptPartProviderMetadata(t *testing.T) {
	var prompt interface{}
	if err := json.Unmarshal([]byte(`[{"role":"assistant","content":[
		{"type":"tool-call","toolCallId":"c1","toolName":"tool_search","input":{"call_id":null},"providerExecuted":true,"providerMetadata":{"openai":{"itemId":"tsc_1"}}},
		{"type":"tool-result","toolCallId":"c1","toolName":"tool_search","output":{"type":"json","value":{"tools":[]}},"providerMetadata":{"openai":{"itemId":"tso_1"}}},
		{"type":"tool-result","toolCallId":"c2","toolName":"x","output":{"type":"text","value":"v"}}]}]`), &prompt); err != nil {
		t.Fatal(err)
	}
	msgs, reason := BuildPromptMessages(prompt)
	if reason != "" || len(msgs) != 1 || len(msgs[0].Content) != 3 {
		t.Fatalf("msgs = %#v, reason %q", msgs, reason)
	}
	tc, _ := msgs[0].Content[0].(types.ToolCallContent)
	tr, _ := msgs[0].Content[1].(types.ToolResultContent)
	plain, _ := msgs[0].Content[2].(types.ToolResultContent)
	if string(tc.ProviderMetadata) != `{"openai":{"itemId":"tsc_1"}}` || !tc.ProviderExecuted {
		t.Errorf("tool-call = %#v", tc)
	}
	if string(tr.ProviderMetadata) != `{"openai":{"itemId":"tso_1"}}` {
		t.Errorf("tool-result providerMetadata = %s", tr.ProviderMetadata)
	}
	if plain.ProviderMetadata != nil {
		t.Errorf("tool-result without metadata = %s", plain.ProviderMetadata)
	}
}
