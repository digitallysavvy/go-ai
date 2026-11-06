package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convert-to-openai-responses-input.ts (store: false): reasoning parts
// replay any `reasoningEncryptedContent != null`, "" included, and the final
// filter drops only items whose encrypted_content == null.
func TestR78provbReasoningEncryptedContentNullAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name      string
		part      types.ReasoningContent
		wantItems int
		wantKey   bool
		want      interface{}
	}{
		{"item id, null", types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"itemId": "rs_1", "reasoningEncryptedContent": nil}}}, 0, false, nil},
		{"item id, empty", types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"itemId": "rs_3", "reasoningEncryptedContent": ""}}}, 1, true, ""},
		{"no item id, empty", types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEncryptedContent": ""}}}, 1, true, ""},
		{"no item id, null", types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEncryptedContent": nil}}}, 0, false, nil},
	} {
		input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{tc.part}}}}, "system", ConvertOptions{Store: false})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(input) != tc.wantItems {
			t.Fatalf("%s: %d input items, want %d (%v)", tc.name, len(input), tc.wantItems, input)
		}
		if tc.wantItems == 0 {
			continue
		}
		got, has := input[0].(map[string]interface{})["encrypted_content"]
		if has != tc.wantKey || got != tc.want {
			t.Errorf("%s: encrypted_content = %#v (present %v), want %#v (present %v)", tc.name, got, has, tc.want, tc.wantKey)
		}
	}
}
