package openresponses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertToOpenResponsesInput converts AI SDK messages to Open Responses format
func ConvertToOpenResponsesInput(messages []types.Message, system string) (interface{}, string, []types.Warning) {
	var input []interface{}
	var warnings []types.Warning
	var systemMessages []string

	// Collect system messages
	if system != "" {
		systemMessages = append(systemMessages, system)
	}

	// Convert each message
	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			// System messages become instructions - extract text from content parts
			for _, part := range msg.Content {
				if textContent, ok := part.(types.TextContent); ok {
					systemMessages = append(systemMessages, textContent.Text)
				}
			}

		case types.RoleUser:
			userContent := convertUserContent(msg.Content, &warnings)
			input = append(input, MessageItem{
				Type:    "message",
				Role:    "user",
				Content: userContent,
			})

		case types.RoleAssistant:
			assistantContent, toolCalls := convertAssistantContent(msg.Content)

			// Add assistant message if it has text content
			if len(assistantContent) > 0 {
				input = append(input, MessageItem{
					Type:    "message",
					Role:    "assistant",
					Content: assistantContent,
				})
			}

			// Add tool calls as separate items
		input = append(input, toolCalls...)

		case types.RoleTool:
			// Convert tool results
			toolResults := convertToolResults(msg.Content, &warnings)
			input = append(input, toolResults...)
		}
	}

	// Combine system messages into instructions
	var instructions string
	if len(systemMessages) > 0 {
		instructions = strings.Join(systemMessages, "\n")
	}

	return input, instructions, warnings
}

// convertUserContent converts user message content to Open Responses format
func convertUserContent(content []types.ContentPart, warnings *[]types.Warning) []interface{} {
	var result []interface{}

	for _, part := range content {
		switch p := part.(type) {
		case types.TextContent:
			result = append(result, InputTextContent{
				Type: "input_text",
				Text: p.Text,
			})

		case types.ImageContent:
			imageURL := convertImageContentToURL(p, warnings)
			if imageURL != "" {
				result = append(result, InputImageContent{
					Type:     "input_image",
					ImageURL: imageURL,
				})
			}

		case types.FileContent:
			// Handle file content (converted to image if image type)
			if strings.HasPrefix(p.MimeType, "image/") {
				imageURL := convertFileToImageURL(p, warnings)
				if imageURL != "" {
					result = append(result, InputImageContent{
						Type:     "input_image",
						ImageURL: imageURL,
					})
				}
			} else {
				*warnings = append(*warnings, types.Warning{
					Type:    "unsupported-content",
					Message: fmt.Sprintf("unsupported file content type: %s", p.MimeType),
				})
			}
		}
	}

	return result
}

// convertImageContentToURL converts ImageContent to a data URL or returns the URL
func convertImageContentToURL(img types.ImageContent, warnings *[]types.Warning) string {
	// Check for URL first
	if img.URL != "" {
		return img.URL
	}

	// Convert data to base64 data URL
	if len(img.Image) > 0 {
		mediaType := img.MimeType
		if mediaType == "" {
			mediaType = "image/jpeg"
		}
		dataStr := base64.StdEncoding.EncodeToString(img.Image)
		return fmt.Sprintf("data:%s;base64,%s", mediaType, dataStr)
	}

	return ""
}

// convertFileToImageURL converts FileContent to an image data URL
func convertFileToImageURL(file types.FileContent, warnings *[]types.Warning) string {
	mediaType := file.MimeType
	if mediaType == "" || mediaType == "image/*" {
		mediaType = "image/jpeg"
	}

	if len(file.Data) > 0 {
		dataStr := base64.StdEncoding.EncodeToString(file.Data)
		return fmt.Sprintf("data:%s;base64,%s", mediaType, dataStr)
	}

	return ""
}

// convertAssistantContent converts assistant message content
func convertAssistantContent(content []types.ContentPart) ([]interface{}, []interface{}) {
	var textContent []interface{}
	var toolCalls []interface{}

	for _, part := range content {
		contentType := part.ContentType()

		switch contentType {
		case "text":
			if textPart, ok := part.(types.TextContent); ok {
				textContent = append(textContent, OutputTextContent{
					Type: "output_text",
					Text: textPart.Text,
				})
			}

		case "reasoning":
			// Emit reasoning as a top-level reasoning input item, not as output_text.
			// Only forward when EncryptedContent is present; without it the API cannot
			// reconstruct the reasoning context for multi-turn conversations (#12869).
			if reasoningPart, ok := part.(types.ReasoningContent); ok {
				if reasoningPart.EncryptedContent != "" {
					item := ReasoningInputItem{
						Type:             "reasoning",
						EncryptedContent: reasoningPart.EncryptedContent,
					}
					if reasoningPart.Text != "" {
						item.Summary = []SummaryPart{{Type: "summary_text", Text: reasoningPart.Text}}
					}
					toolCalls = append(toolCalls, item)
				}
				// No EncryptedContent: skip (e.g. Anthropic reasoning blocks, or
				// reasoning from a provider that doesn't use this field).
			}
		}
	}

	return textContent, toolCalls
}

// convertToolResults converts tool results to Open Responses format
func convertToolResults(content []types.ContentPart, warnings *[]types.Warning) []interface{} {
	var results []interface{}

	for _, part := range content {
		if part.ContentType() == "tool-result" {
			if toolResult, ok := part.(types.ToolResultContent); ok {
				output := convertToolResultOutput(toolResult, warnings)

				results = append(results, FunctionCallOutputItem{
					Type:   "function_call_output",
					CallID: toolResult.ToolCallID,
					Output: output,
				})
			}
		}
	}

	return results
}

// convertToolResultOutput converts tool result output to appropriate format
func convertToolResultOutput(toolResult types.ToolResultContent, warnings *[]types.Warning) interface{} {
	// If there's an error, return the error message
	if toolResult.Error != "" {
		return toolResult.Error
	}

	// If result is nil, return empty string
	if toolResult.Result == nil {
		return ""
	}

	// Try to convert result to string if it's a simple type
	switch v := toolResult.Result.(type) {
	case string:
		return v
	case int, int32, int64, float32, float64, bool:
		return fmt.Sprintf("%v", v)
	default:
		// For complex types, JSON encode
		jsonBytes, _ := json.Marshal(v)
		return string(jsonBytes)
	}
}
