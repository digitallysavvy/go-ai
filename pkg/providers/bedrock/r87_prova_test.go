package bedrock

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from convert-to-amazon-bedrock-chat-messages.test.ts "foreign
// provider history" (TS fd7e8a75d7).
func TestR87provaBedrockForeignEmptySignatureText(t *testing.T) {
	asJSON := func(v interface{}) interface{} {
		b, _ := json.Marshal(v)
		var out interface{}
		_ = json.Unmarshal(b, &out)
		return out
	}
	for _, namespace := range []string{"google", "vertex", "googleVertex"} {
		sig := func(s string) map[string]interface{} {
			return map[string]interface{}{namespace: map[string]interface{}{"thoughtSignature": s}}
		}
		_, messages, err := convertToBedrockChatMessages([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "Answer  "},
			types.TextContent{Text: "", ProviderOptions: sig("opaque")},
		}}}, false)
		if err != nil {
			t.Fatal(err)
		}
		want := asJSON([]interface{}{map[string]interface{}{"role": "assistant", "content": []interface{}{map[string]interface{}{"text": "Answer"}}}})
		if got := asJSON(messages); !reflect.DeepEqual(got, want) {
			t.Errorf("%s trim: messages = %v", namespace, got)
		}

		_, messages, err = convertToBedrockChatMessages([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ReasoningContent{Text: "Foreign reasoning", ProviderOptions: sig("reasoning")},
			types.TextContent{Text: "A"},
			types.TextContent{Text: "", ProviderOptions: sig("opaque")},
			types.TextContent{Text: "B"},
		}}}, false)
		if err != nil {
			t.Fatal(err)
		}
		want = asJSON([]interface{}{map[string]interface{}{"role": "assistant", "content": []interface{}{map[string]interface{}{"text": "A"}, map[string]interface{}{"text": "B"}}}})
		if got := asJSON(messages); !reflect.DeepEqual(got, want) {
			t.Errorf("%s reasoning: messages = %v", namespace, got)
		}
	}
	// Native empty text (bedrock key) next to reasoning is kept, as before.
	_, messages, err := convertToBedrockChatMessages([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ReasoningContent{Text: "r", Signature: "s"},
		types.TextContent{Text: "", ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{}}},
		types.TextContent{Text: "B"},
	}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := asJSON(messages); len(got.([]interface{})[0].(map[string]interface{})["content"].([]interface{})) != 3 {
		t.Errorf("native empty text dropped: %v", got)
	}
}
