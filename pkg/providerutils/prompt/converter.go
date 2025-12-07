package prompt

import (
	"encoding/base64"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToOpenAIMessages converts unified messages to OpenAI format
func ToOpenAIMessages(messages []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	for _, msg := range messages {
		openAIMsg := map[string]interface{}{
			"role": string(msg.Role),
		}

		// Handle content
		if len(msg.Content) == 1 && msg.Content[0].ContentType() == "text" {
			// Simple text content
			if textContent, ok := msg.Content[0].(types.TextContent); ok {
				openAIMsg["content"] = textContent.Text
			}
		} else {
			// Multi-part content
			contentParts := make([]map[string]interface{}, 0, len(msg.Content))
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					contentParts = append(contentParts, map[string]interface{}{
						"type": "text",
						"text": p.Text,
					})
				case types.ImageContent:
					// Convert image to base64 if it's raw bytes
					var imageData string
					if p.URL != "" {
						imageData = p.URL
					} else {
						imageData = fmt.Sprintf("data:%s;base64,%s",
							p.MimeType, base64.StdEncoding.EncodeToString(p.Image))
					}
					contentParts = append(contentParts, map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": imageData,
						},
					})
				case types.ToolResultContent:
					// OpenAI doesn't have a separate tool result content type
					// Represent as text
					contentParts = append(contentParts, map[string]interface{}{
						"type": "text",
						"text": fmt.Sprintf("Tool %s result: %v", p.ToolName, p.Result),
					})
				}
			}
			openAIMsg["content"] = contentParts
		}

		// Add name if present
		if msg.Name != "" {
			openAIMsg["name"] = msg.Name
		}

		result = append(result, openAIMsg)
	}

	return result
}

// ToAnthropicMessages converts unified messages to Anthropic format
func ToAnthropicMessages(messages []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	for _, msg := range messages {
		// Skip system messages (handled separately in Anthropic)
		if msg.Role == types.RoleSystem {
			continue
		}

		anthropicMsg := map[string]interface{}{
			"role": string(msg.Role),
		}

		// Handle content
		if len(msg.Content) == 1 && msg.Content[0].ContentType() == "text" {
			// Simple text content
			if textContent, ok := msg.Content[0].(types.TextContent); ok {
				anthropicMsg["content"] = textContent.Text
			}
		} else {
			// Multi-part content
			contentParts := make([]map[string]interface{}, 0, len(msg.Content))
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					contentParts = append(contentParts, map[string]interface{}{
						"type": "text",
						"text": p.Text,
					})
				case types.ImageContent:
					// Anthropic requires base64 encoded images
					imageData := base64.StdEncoding.EncodeToString(p.Image)
					contentParts = append(contentParts, map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": p.MimeType,
							"data":       imageData,
						},
					})
				case types.ToolResultContent:
					contentParts = append(contentParts, map[string]interface{}{
						"type":         "tool_result",
						"tool_use_id":  p.ToolCallID,
						"content":      fmt.Sprintf("%v", p.Result),
						"is_error":     p.Error != "",
					})
				}
			}
			anthropicMsg["content"] = contentParts
		}

		result = append(result, anthropicMsg)
	}

	return result
}

// ExtractSystemMessage extracts the system message from a list of messages
// Used for providers that handle system messages separately (like Anthropic)
func ExtractSystemMessage(messages []types.Message) string {
	for _, msg := range messages {
		if msg.Role == types.RoleSystem && len(msg.Content) > 0 {
			if textContent, ok := msg.Content[0].(types.TextContent); ok {
				return textContent.Text
			}
		}
	}
	return ""
}

// ToGoogleMessages converts unified messages to Google (Gemini) format
func ToGoogleMessages(messages []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	for _, msg := range messages {
		// Map role to Google format
		role := "user"
		if msg.Role == types.RoleAssistant {
			role = "model"
		}

		googleMsg := map[string]interface{}{
			"role": role,
		}

		// Handle content
		parts := make([]map[string]interface{}, 0, len(msg.Content))
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.TextContent:
				parts = append(parts, map[string]interface{}{
					"text": p.Text,
				})
			case types.ImageContent:
				// Google expects base64 encoded images
				imageData := base64.StdEncoding.EncodeToString(p.Image)
				parts = append(parts, map[string]interface{}{
					"inline_data": map[string]interface{}{
						"mime_type": p.MimeType,
						"data":      imageData,
					},
				})
			}
		}

		googleMsg["parts"] = parts
		result = append(result, googleMsg)
	}

	return result
}

// SimpleTextToMessages converts a simple text prompt to a message list
func SimpleTextToMessages(text string) []types.Message {
	return []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: text},
			},
		},
	}
}

// MessagesToSimpleText converts a message list to simple text
// This is a lossy conversion and only works for simple text-only conversations
func MessagesToSimpleText(messages []types.Message) string {
	var result string
	for _, msg := range messages {
		for _, part := range msg.Content {
			if textContent, ok := part.(types.TextContent); ok {
				if result != "" {
					result += "\n"
				}
				result += textContent.Text
			}
		}
	}
	return result
}

// AddToolResultsToMessages adds tool results to a message list
func AddToolResultsToMessages(messages []types.Message, toolResults []types.ToolResult) []types.Message {
	if len(toolResults) == 0 {
		return messages
	}

	// Create content parts for tool results
	contentParts := make([]types.ContentPart, len(toolResults))
	for i, result := range toolResults {
		contentParts[i] = types.ToolResultContent{
			ToolCallID: result.ToolCallID,
			ToolName:   result.ToolName,
			Result:     result.Result,
		}
	}

	// Add as a tool message
	return append(messages, types.Message{
		Role:    types.RoleTool,
		Content: contentParts,
	})
}

// ValidateMessages validates that messages are well-formed
func ValidateMessages(messages []types.Message) error {
	if len(messages) == 0 {
		return fmt.Errorf("messages cannot be empty")
	}

	for i, msg := range messages {
		if msg.Role == "" {
			return fmt.Errorf("message %d has empty role", i)
		}
		if len(msg.Content) == 0 {
			return fmt.Errorf("message %d has empty content", i)
		}
	}

	return nil
}
