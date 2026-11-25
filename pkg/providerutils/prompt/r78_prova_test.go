package prompt

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r78provaReasoningPrompt(anthropic interface{}) []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ReasoningContent{Text: "thought", ProviderOptions: map[string]interface{}{"anthropic": anthropic}},
			types.TextContent{Text: "answer"},
		}},
	}
}

// TS parseProviderOptions(anthropicReasoningMetadataSchema) throws an
// InvalidArgumentError for a mistyped signature/redactedData (null included)
// or a non-object providerOptions.anthropic.
func TestR78prova_AnthropicReasoningOptionsValidated(t *testing.T) {
	for name, anthropic := range map[string]interface{}{
		"numeric signature":  map[string]interface{}{"signature": 1},
		"null signature":     map[string]interface{}{"signature": nil},
		"null redactedData":  map[string]interface{}{"redactedData": nil},
		"bool redactedData":  map[string]interface{}{"redactedData": true},
		"non-object options": "x",
	} {
		_, err := ConvertToAnthropicPrompt(r78provaReasoningPrompt(anthropic), AnthropicPromptOptions{})
		var iae *providererrors.InvalidArgumentError
		if !errors.As(err, &iae) || iae.Message != "invalid anthropic provider options" {
			t.Errorf("%s: err = %v, want InvalidArgumentError", name, err)
		}
	}

	// A typed Go map is an object: its signature is replayed.
	out, err := ConvertToAnthropicPrompt(r78provaReasoningPrompt(map[string]string{"signature": "sig"}), AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content := out.Messages[1]["content"].([]map[string]interface{})
	if content[0]["type"] != "thinking" || content[0]["signature"] != "sig" {
		t.Fatalf("typed map: content = %v", content)
	}
	// A null providerOptions.anthropic is no options (TS == null), not an error.
	if _, err := ConvertToAnthropicPrompt(r78provaReasoningPrompt(nil), AnthropicPromptOptions{}); err != nil {
		t.Fatalf("nil anthropic options: %v", err)
	}
}
