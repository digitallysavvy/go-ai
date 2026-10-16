package openresponses

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS compares a reasoning item with the last input item, so same-id reasoning
// at the start of a following assistant message merges into the previous one.
func TestConvert_ReasoningMergesAcrossAssistantMessages(t *testing.T) {
	r := func(text string) types.ContentPart {
		return types.ReasoningContent{Text: text, ProviderMetadata: json.RawMessage(`{"openai":{"itemId":"rs_1"}}`)}
	}
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{r("a")}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{r("b")}},
	}
	out, _, _, err := ConvertToOpenResponsesInputForProviderStrict(msgs, "", "openai", false)
	if err != nil {
		t.Fatal(err)
	}
	items := out.([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 merged item, got %d: %+v", len(items), items)
	}
	if c := items[0].(ReasoningInputItem).Content; len(c) != 2 || c[0].Text != "a" || c[1].Text != "b" {
		t.Fatalf("content = %+v", c)
	}
}
