package acp

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TextContentBlock is the only prompt content block this port supports.
// Mirrors TS `ACPTextContentBlock` (the "text" variant of ACPContentBlock).
type TextContentBlock struct {
	Type string `json:"type"` // always "text"
	Text string `json:"text"`
}

// convertPromptToTextBlocks mirrors TS
// `convertHarnessPromptToACPTextBlocks`.
func convertPromptToTextBlocks(prompt harness.Prompt, harnessID string) ([]TextContentBlock, error) {
	if prompt.Message == nil {
		return []TextContentBlock{{Type: "text", Text: prompt.Text}}, nil
	}
	var blocks []TextContentBlock
	for _, part := range prompt.Message.Content {
		switch p := part.(type) {
		case types.TextContent:
			blocks = append(blocks, TextContentBlock{Type: "text", Text: p.Text})
		case types.ImageContent:
			return nil, unsupportedPromptContent(harnessID, "image content")
		case types.FileContent:
			category := strings.ToLower(strings.SplitN(p.MediaType, "/", 2)[0])
			switch category {
			case "image":
				return nil, unsupportedPromptContent(harnessID, "image content")
			case "audio":
				return nil, unsupportedPromptContent(harnessID, "audio content")
			default:
				return nil, unsupportedPromptContent(harnessID, fmt.Sprintf("embedded resource content with media type %q", p.MediaType))
			}
		default:
			return nil, unsupportedPromptContent(harnessID, fmt.Sprintf("content parts of type %q", part.ContentType()))
		}
	}
	return blocks, nil
}

func unsupportedPromptContent(harnessID, category string) error {
	return harness.NewCapabilityUnsupportedError(
		fmt.Sprintf("The %s ACP harness supports only portable text prompts and does not support %s. Pass a string or a user message whose content contains only text parts.", harnessID, category),
		harnessID, nil)
}

// prependInstructionGuidance mirrors TS `prependACPInstructionGuidance`.
func prependInstructionGuidance(prompt []TextContentBlock, instructions string) []TextContentBlock {
	if instructions == "" {
		return append([]TextContentBlock(nil), prompt...)
	}
	lines := []string{
		"<session-guidance>",
		"This block is operating guidance from the harness, not user-authored content.",
		"<instructions>", instructions, "</instructions>",
		"</session-guidance>",
	}
	out := make([]TextContentBlock, 0, len(prompt)+1)
	out = append(out, TextContentBlock{Type: "text", Text: strings.Join(lines, "\n")})
	out = append(out, prompt...)
	return out
}
