package responses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertPromptToInput converts a types.Prompt to the Responses API input slice.
// systemMessageMode must be "system", "developer", or "remove".
//
// The resulting slice is suitable for use as the "input" field in a Responses
// API request body. Each element is one of:
//   - SystemMessage (system/developer role)
//   - UserMessage
//   - AssistantMessageItem
//   - FunctionCallItem
//   - FunctionCallOutputItem
func ConvertPromptToInput(prompt types.Prompt, systemMessageMode string) []interface{} {
	input := make([]interface{}, 0, len(prompt.Messages)+1)

	// Prepend system message when present and not suppressed.
	if prompt.System != "" && systemMessageMode != "remove" {
		input = append(input, SystemMessage{
			Role:    systemMessageMode,
			Content: prompt.System,
		})
	}

	for _, msg := range prompt.Messages {
		switch msg.Role {
		case types.RoleUser:
			input = append(input, convertUserMessage(msg))
		case types.RoleAssistant:
			input = append(input, convertAssistantItems(msg)...)
		case types.RoleTool:
			input = append(input, convertToolItems(msg)...)
		}
	}

	return input
}

// convertUserMessage maps a user-role Message to a UserMessage.
// Simple single-text messages use a string content value; multi-modal messages
// use a slice of typed content parts.
func convertUserMessage(msg types.Message) UserMessage {
	if len(msg.Content) == 1 {
		if text, ok := msg.Content[0].(types.TextContent); ok {
			return UserMessage{Role: "user", Content: text.Text}
		}
	}

	parts := make([]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			parts = append(parts, UserTextPart{Type: "input_text", Text: p.Text})
		case types.ImageContent:
			imageURL := p.URL
			if imageURL == "" && len(p.Image) > 0 {
				imageURL = fmt.Sprintf("data:%s;base64,%s",
					p.MimeType, base64.StdEncoding.EncodeToString(p.Image))
			}
			if imageURL != "" {
				parts = append(parts, UserImageURLPart{Type: "input_image", ImageURL: imageURL})
			}
		}
	}

	return UserMessage{Role: "user", Content: parts}
}

// convertAssistantItems maps an assistant-role Message to one or more Responses
// API items. Text content becomes an AssistantMessageItem; each tool call in
// Message.ToolCalls becomes a separate FunctionCallItem.
func convertAssistantItems(msg types.Message) []interface{} {
	items := make([]interface{}, 0, 1+len(msg.ToolCalls))

	// Collect text content parts.
	textParts := make([]AssistantMessageContent, 0)
	for _, part := range msg.Content {
		if text, ok := part.(types.TextContent); ok {
			textParts = append(textParts, AssistantMessageContent{
				Type: "output_text",
				Text: text.Text,
			})
		}
	}
	if len(textParts) > 0 {
		items = append(items, AssistantMessageItem{
			Type:    "message",
			Role:    "assistant",
			Content: textParts,
		})
	}

	// Each ToolCall on the message becomes a function_call item.
	for _, tc := range msg.ToolCalls {
		argsJSON, _ := json.Marshal(tc.Arguments)
		items = append(items, FunctionCallItem{
			Type:      "function_call",
			ID:        tc.ID,
			CallID:    tc.ID,
			Name:      tc.ToolName,
			Arguments: string(argsJSON),
		})
	}

	return items
}

// convertToolItems maps tool-role message content to function_call_output items.
func convertToolItems(msg types.Message) []interface{} {
	items := make([]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		if tr, ok := part.(types.ToolResultContent); ok {
			items = append(items, FunctionCallOutputItem{
				Type:   "function_call_output",
				CallID: tr.ToolCallID,
				Output: toolResultOutput(tr),
			})
		}
	}
	return items
}

// toolResultOutput converts a ToolResultContent to the Responses API output value.
// Returns a plain string for text/json outputs, or a []CustomToolCallOutputPart for
// content-array outputs (text, image-data, image-url, file-data, file-url).
func toolResultOutput(tr types.ToolResultContent) interface{} {
	if tr.Output != nil {
		switch tr.Output.Type {
		case types.ToolResultOutputText:
			if s, ok := tr.Output.Value.(string); ok {
				return s
			}
		case types.ToolResultOutputJSON:
			if b, err := json.Marshal(tr.Output.Value); err == nil {
				return string(b)
			}
		case types.ToolResultOutputExecutionDenied:
			if tr.Output.Reason != "" {
				return tr.Output.Reason
			}
			return "Tool execution denied."
		case types.ToolResultOutputContent:
			parts := make([]CustomToolCallOutputPart, 0, len(tr.Output.Content))
			for _, block := range tr.Output.Content {
				switch b := block.(type) {
				case types.TextContentBlock:
					parts = append(parts, CustomToolCallOutputPart{
						Type: "input_text",
						Text: b.Text,
					})
				case types.ImageContentBlock:
					imageURL := fmt.Sprintf("data:%s;base64,%s",
						b.MediaType, base64.StdEncoding.EncodeToString(b.Data))
					parts = append(parts, CustomToolCallOutputPart{
						Type:     "input_image",
						ImageURL: imageURL,
					})
				case types.FileContentBlock:
					if b.URL != "" {
						// file-url: remote URL reference
						parts = append(parts, CustomToolCallOutputPart{
							Type:    "input_file",
							FileURL: b.URL,
						})
					} else if len(b.Data) > 0 {
						// file-data: inline base64
						fileData := fmt.Sprintf("data:%s;base64,%s",
							b.MediaType, base64.StdEncoding.EncodeToString(b.Data))
						filename := b.Filename
						if filename == "" {
							filename = "data"
						}
						parts = append(parts, CustomToolCallOutputPart{
							Type:     "input_file",
							Filename: filename,
							FileData: fileData,
						})
					}
				}
			}
			if len(parts) > 0 {
				return parts
			}
		}
	}
	if s, ok := tr.Result.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", tr.Result)
}
