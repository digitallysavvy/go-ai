package openai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TS emits a reasoning part for an item with an empty id and no encrypted_content,
// with providerMetadata {openai: {itemId: "", reasoningEncryptedContent: null}}.
func TestResponsesConvertResponse_ReasoningItemWithoutIDOrEncryptedContent(t *testing.T) {
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5")
	item := json.RawMessage(`{"type":"reasoning","id":"","summary":[]}`)
	result, err := model.convertResponse(responses.ResponsesAPIResponse{
		ID: "resp_1", Model: "gpt-5", Output: []json.RawMessage{item},
		Usage: &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
	}, true, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v", result.Content)
	}
	r, ok := result.Content[0].(types.ReasoningContent)
	if !ok {
		t.Fatalf("content[0] = %#v", result.Content[0])
	}
	if got := string(r.ProviderMetadata); got != `{"openai":{"itemId":"","reasoningEncryptedContent":null}}` {
		t.Fatalf("providerMetadata = %s", got)
	}
}
