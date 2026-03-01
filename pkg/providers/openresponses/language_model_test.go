package openresponses

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BUG-T12: reasoning output items that have encrypted_content but no ID must still
// be included in the response text (#12869).
func TestConvertResponse_ReasoningWithEncryptedContentNoID(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				// No ID — the item arrives without one, but has encrypted_content.
				Type:             "reasoning",
				EncryptedContent: "enc-abc123",
				Summary: []ContentPart{
					{Type: "summary_text", Text: "thinking step"},
				},
			},
			{
				Type: "message",
				Content: []ContentPart{
					{Type: "output_text", Text: "final answer"},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result := lm.convertResponse(response)

	// The reasoning text should appear in the combined output.
	if !strings.Contains(result.Text, "thinking step") {
		t.Errorf("expected reasoning summary in text, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "final answer") {
		t.Errorf("expected message text in output, got %q", result.Text)
	}
}

// TestConvertResponse_ReasoningWithIDNoEncryptedContent verifies that a reasoning item
// with an ID (but no encrypted_content) is still included.
func TestConvertResponse_ReasoningWithIDNoEncryptedContent(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				ID:   "item_001",
				Type: "reasoning",
				Summary: []ContentPart{
					{Type: "text", Text: "reasoning text"},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result := lm.convertResponse(response)

	if !strings.Contains(result.Text, "reasoning text") {
		t.Errorf("expected reasoning text in output, got %q", result.Text)
	}
}

// TestConvertResponse_ReasoningWithNoIDNoEncryptedContent verifies that a reasoning
// item with neither ID nor encrypted_content is skipped.
func TestConvertResponse_ReasoningWithNoIDNoEncryptedContent(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				// Neither ID nor EncryptedContent — should be ignored.
				Type: "reasoning",
				Summary: []ContentPart{
					{Type: "summary_text", Text: "should be ignored"},
				},
			},
			{
				Type: "message",
				Content: []ContentPart{
					{Type: "output_text", Text: "only this"},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result := lm.convertResponse(response)

	if strings.Contains(result.Text, "should be ignored") {
		t.Errorf("reasoning item without ID or encrypted_content should be skipped, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "only this") {
		t.Errorf("expected message text in output, got %q", result.Text)
	}
}

// TestConvertResponse_ReasoningPopulatesContentWithEncryptedContent verifies
// that convertResponse stores the reasoning block in result.Content as a
// ReasoningContent with EncryptedContent set, enabling the input-side
// round-trip on subsequent turns (#12869).
func TestConvertResponse_ReasoningPopulatesContentWithEncryptedContent(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				Type:             "reasoning",
				EncryptedContent: "enc-round-trip",
				Summary: []ContentPart{
					{Type: "summary_text", Text: "my reasoning"},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result := lm.convertResponse(response)

	// result.Content must contain exactly one ReasoningContent part.
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(result.Content))
	}
	rc, ok := result.Content[0].(types.ReasoningContent)
	if !ok {
		t.Fatalf("expected ReasoningContent, got %T", result.Content[0])
	}
	if rc.EncryptedContent != "enc-round-trip" {
		t.Errorf("EncryptedContent = %q, want %q", rc.EncryptedContent, "enc-round-trip")
	}
	if rc.Text != "my reasoning" {
		t.Errorf("Text = %q, want %q", rc.Text, "my reasoning")
	}
}

// TestConvertResponse_ReasoningNoContentWhenNeitherIDNorEncrypted verifies
// that result.Content is empty when the reasoning item has neither ID nor
// EncryptedContent (the item is skipped entirely).
func TestConvertResponse_ReasoningNoContentWhenNeitherIDNorEncrypted(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{Type: "reasoning", Summary: []ContentPart{{Type: "summary_text", Text: "ignore me"}}},
		},
	}

	lm := &LanguageModel{}
	result := lm.convertResponse(response)

	if len(result.Content) != 0 {
		t.Errorf("expected no content parts, got %d", len(result.Content))
	}
}
