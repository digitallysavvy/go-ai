package prompt

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r87provaJSON(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

// Ported from convert-to-google-messages.test.ts (TS fd7e8a75d7): an empty
// text or reasoning part that carries a thoughtSignature is kept.
func TestR87provaGoogleStandaloneEmptySignatures(t *testing.T) {
	for _, namespace := range []string{"google", "vertex", "googleVertex"} {
		msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "", ProviderOptions: map[string]interface{}{namespace: map[string]interface{}{"thoughtSignature": "text-signature"}}},
			types.ReasoningContent{Text: "", ProviderOptions: map[string]interface{}{namespace: map[string]interface{}{"thoughtSignature": "reasoning-signature"}}},
		}}}
		out, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IncludeFunctionCallIDs: true, SupportsFunctionResponseParts: true, ProviderOptionsNames: []string{"googleVertex", "vertex"}})
		if err != nil {
			t.Fatal(err)
		}
		want := r87provaJSON(t, []interface{}{map[string]interface{}{"role": "model", "parts": []interface{}{
			map[string]interface{}{"text": "", "thoughtSignature": "text-signature"},
			map[string]interface{}{"text": "", "thought": true, "thoughtSignature": "reasoning-signature"},
		}}})
		if got := r87provaJSON(t, out.Contents); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: contents = %v", namespace, got)
		}
	}
	// Without a signature empty text is still dropped.
	out, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: ""}, types.TextContent{Text: "A"}}}}, GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := r87provaJSON(t, out.Contents); !reflect.DeepEqual(got, r87provaJSON(t, []interface{}{map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "A"}}}})) {
		t.Errorf("contents = %v", got)
	}
}

// Ported from convert-to-anthropic-prompt.test.ts "foreign provider history"
// (TS fd7e8a75d7): empty text is removed before the last part is chosen, so
// message cache control and the prefill trim reach the last sent part.
func TestR87provaAnthropicForeignEmptySignatureText(t *testing.T) {
	yes := true
	for _, namespace := range []string{"google", "vertex", "googleVertex"} {
		sig := map[string]interface{}{namespace: map[string]interface{}{"thoughtSignature": "opaque"}}
		out, err := ConvertToAnthropicPrompt([]types.Message{{
			Role:            types.RoleAssistant,
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}},
			Content: []types.ContentPart{
				types.TextContent{Text: "Answer  "},
				types.TextContent{Text: "", ProviderOptions: sig},
			},
		}}, AnthropicPromptOptions{SendReasoning: &yes})
		if err != nil {
			t.Fatal(err)
		}
		want := r87provaJSON(t, []interface{}{map[string]interface{}{"role": "assistant", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "Answer", "cache_control": map[string]interface{}{"type": "ephemeral"}},
		}}})
		if got := r87provaJSON(t, out.Messages); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: messages = %v", namespace, got)
		}
		if len(out.Warnings) != 0 {
			t.Errorf("%s: warnings = %v", namespace, out.Warnings)
		}

		out, err = ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "A"},
			types.TextContent{Text: "", ProviderOptions: sig},
			types.TextContent{Text: "B"},
		}}}, AnthropicPromptOptions{SendReasoning: &yes})
		if err != nil {
			t.Fatal(err)
		}
		want = r87provaJSON(t, []interface{}{map[string]interface{}{"role": "assistant", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "A"},
			map[string]interface{}{"type": "text", "text": "B"},
		}}})
		if got := r87provaJSON(t, out.Messages); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: messages = %v", namespace, got)
		}
	}
}
