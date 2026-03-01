package openresponses

import (
	"strings"
	"testing"
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
