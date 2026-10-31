package acp

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ContentBlock is one ACP prompt content block: a text block or an inline
// base64 image block. Mirrors TS `ACPContentBlock`
// (`{ type: 'text', text } | { type: 'image', data, mimeType }`).
type ContentBlock struct {
	// Type is "text" or "image".
	Type string `json:"type"`
	// Text is the text of a "text" block.
	Text string `json:"text,omitempty"`
	// Data is the base64 payload of an "image" block.
	Data string `json:"data,omitempty"`
	// MimeType is the media type of an "image" block (e.g. "image/png").
	MimeType string `json:"mimeType,omitempty"`
}

// TextContentBlock is the text-only prompt block of earlier releases.
//
// Deprecated: prompts can now carry inline images. Use ContentBlock, which
// TextContentBlock aliases.
type TextContentBlock = ContentBlock

// MarshalJSON emits the TS wire shape: a text block always carries `text`
// (even when empty), an image block carries `data` and `mimeType`.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	if b.Type == "image" {
		return json.Marshal(struct {
			Type     string `json:"type"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		}{b.Type, b.Data, b.MimeType})
	}
	return json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{b.Type, b.Text})
}

// convertPromptToContentBlocks mirrors TS
// `convertHarnessPromptToACPContentBlocks`: text parts become text blocks and
// inline images become image blocks, in order. Anything else (remote image
// URLs, provider references, non-image files, other part types) is rejected
// with a *harness.CapabilityUnsupportedError naming harnessID.
func convertPromptToContentBlocks(prompt harness.Prompt, harnessID string) ([]ContentBlock, error) {
	content, err := harnessutil.ExtractPromptContent(prompt)
	if err != nil {
		var unsupported *harness.CapabilityUnsupportedError
		if errors.As(err, &unsupported) {
			return nil, harness.NewCapabilityUnsupportedError(unsupported.Message, harnessID, unsupported)
		}
		return nil, err
	}
	if content.IsText() {
		// A bare string is one block. A text-only message keeps one block per
		// part (the helper joined them, so read the parts from the message).
		if prompt.Message == nil {
			return []ContentBlock{{Type: "text", Text: content.Text}}, nil
		}
		blocks := make([]ContentBlock, 0, len(prompt.Message.Content))
		for _, part := range prompt.Message.Content {
			switch p := part.(type) {
			case types.TextContent:
				blocks = append(blocks, ContentBlock{Type: "text", Text: p.Text})
			case *types.TextContent:
				blocks = append(blocks, ContentBlock{Type: "text", Text: p.Text})
			}
		}
		return blocks, nil
	}
	blocks := make([]ContentBlock, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part.Type == "text" {
			blocks = append(blocks, ContentBlock{Type: "text", Text: part.Text})
		} else {
			blocks = append(blocks, ContentBlock{Type: "image", Data: part.Data, MimeType: part.MediaType})
		}
	}
	return blocks, nil
}

// prependInstructionGuidance mirrors TS `prependACPInstructionGuidance`.
func prependInstructionGuidance(prompt []ContentBlock, instructions string) []ContentBlock {
	if instructions == "" {
		return append(make([]ContentBlock, 0, len(prompt)), prompt...)
	}
	lines := []string{
		"<session-guidance>",
		"This block is operating guidance from the harness, not user-authored content.",
		"<instructions>", instructions, "</instructions>",
		"</session-guidance>",
	}
	out := make([]ContentBlock, 0, len(prompt)+1)
	out = append(out, ContentBlock{Type: "text", Text: strings.Join(lines, "\n")})
	out = append(out, prompt...)
	return out
}
