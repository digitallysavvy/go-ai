package openresponses

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestConvertAssistantContent_ReasoningWithEncryptedContent verifies that a
// ReasoningContent part with EncryptedContent is emitted as a top-level
// ReasoningInputItem (not as output_text) so the API can use it in the next
// turn (#12869 input-side fix).
func TestConvertAssistantContent_ReasoningWithEncryptedContent(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{
			Text:             "some thinking",
			EncryptedContent: "enc-xyz789",
		},
	}

	textParts, separateItems := convertAssistantContent(content)

	// No text content should be added to the message body.
	if len(textParts) != 0 {
		t.Errorf("expected no text content parts, got %d", len(textParts))
	}

	// One separate top-level item should be emitted.
	if len(separateItems) != 1 {
		t.Fatalf("expected 1 separate item, got %d", len(separateItems))
	}

	item, ok := separateItems[0].(ReasoningInputItem)
	if !ok {
		t.Fatalf("expected ReasoningInputItem, got %T", separateItems[0])
	}
	if item.Type != "reasoning" {
		t.Errorf("item.Type = %q, want %q", item.Type, "reasoning")
	}
	if item.EncryptedContent != "enc-xyz789" {
		t.Errorf("item.EncryptedContent = %q, want %q", item.EncryptedContent, "enc-xyz789")
	}
	if len(item.Summary) != 1 || item.Summary[0].Text != "some thinking" {
		t.Errorf("unexpected summary: %+v", item.Summary)
	}
	if item.Summary[0].Type != "summary_text" {
		t.Errorf("summary part type = %q, want %q", item.Summary[0].Type, "summary_text")
	}
}

// TestConvertAssistantContent_ReasoningWithoutEncryptedContent verifies that a
// ReasoningContent part without EncryptedContent is silently dropped (it may be
// from a non-OpenAI provider that doesn't use encrypted_content).
func TestConvertAssistantContent_ReasoningWithoutEncryptedContent(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{
			Text: "anthropic thinking — no encrypted content",
		},
		types.TextContent{Text: "final answer"},
	}

	textParts, separateItems := convertAssistantContent(content)

	// Text content should appear.
	if len(textParts) != 1 {
		t.Errorf("expected 1 text part, got %d", len(textParts))
	}

	// Reasoning without EncryptedContent must NOT produce a separate item.
	if len(separateItems) != 0 {
		t.Errorf("expected no separate items, got %d: %+v", len(separateItems), separateItems)
	}
}

// TestConvertAssistantContent_ReasoningEmptySummaryWhenNoText verifies that when
// EncryptedContent is set but Text is empty, the Summary array is omitted.
func TestConvertAssistantContent_ReasoningEmptySummaryWhenNoText(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{
			Text:             "",
			EncryptedContent: "enc-empty-text",
		},
	}

	_, separateItems := convertAssistantContent(content)

	if len(separateItems) != 1 {
		t.Fatalf("expected 1 separate item, got %d", len(separateItems))
	}
	item := separateItems[0].(ReasoningInputItem)
	if len(item.Summary) != 0 {
		t.Errorf("expected empty Summary when Text is empty, got %+v", item.Summary)
	}
}

// TestConvertAssistantContent_ReasoningInputItemSerializesCorrectly ensures the
// emitted ReasoningInputItem marshals to the JSON shape required by the API.
func TestConvertAssistantContent_ReasoningInputItemSerializesCorrectly(t *testing.T) {
	content := []types.ContentPart{
		types.ReasoningContent{
			Text:             "step one",
			EncryptedContent: "enc-abc",
		},
	}

	_, separateItems := convertAssistantContent(content)
	if len(separateItems) == 0 {
		t.Fatal("expected a separate item")
	}

	b, err := json.Marshal(separateItems[0])
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if m["type"] != "reasoning" {
		t.Errorf(`type = %v, want "reasoning"`, m["type"])
	}
	if m["encrypted_content"] != "enc-abc" {
		t.Errorf(`encrypted_content = %v, want "enc-abc"`, m["encrypted_content"])
	}
}
