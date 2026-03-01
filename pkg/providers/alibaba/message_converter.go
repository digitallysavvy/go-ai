package alibaba

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertToAlibabaChatMessages converts SDK messages to Alibaba API format.
//
// Alibaba uses an OpenAI-compatible message format. When validator is non-nil,
// cache markers are applied to the last eligible content part of each message,
// enabling prompt caching for system, user, assistant, and tool messages.
// The validator enforces the 4-breakpoint limit and accumulates warnings.
//
// Pass nil for validator to produce compact string content with no caching.
func ConvertToAlibabaChatMessages(messages []types.Message, validator *CacheControlValidator) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	for _, msg := range messages {
		converted := convertMessage(msg, validator)
		if converted != nil {
			result = append(result, converted)
		}
	}

	return result
}

// convertMessage converts a single message to Alibaba API format.
func convertMessage(msg types.Message, validator *CacheControlValidator) map[string]interface{} {
	switch msg.Role {
	case types.RoleSystem:
		return convertSystemMessage(msg, validator)
	case types.RoleUser:
		return convertUserMessage(msg, validator)
	case types.RoleAssistant:
		return convertAssistantMessage(msg, validator)
	case types.RoleTool:
		return convertToolMessage(msg, validator)
	default:
		return nil
	}
}

// convertSystemMessage converts a system message to Alibaba format.
// If validator is non-nil, wraps the content in a content array to attach the cache marker.
func convertSystemMessage(msg types.Message, validator *CacheControlValidator) map[string]interface{} {
	text := extractText(msg.Content)

	if validator != nil {
		cc := validator.GetCacheControl()
		part := map[string]interface{}{
			"type": "text",
			"text": text,
		}
		if cc != nil {
			part["cache_control"] = cc
		}
		return map[string]interface{}{
			"role":    "system",
			"content": []map[string]interface{}{part},
		}
	}

	return map[string]interface{}{
		"role":    "system",
		"content": text,
	}
}

// convertUserMessage converts a user message to Alibaba format.
// Supports text and image content parts.
func convertUserMessage(msg types.Message, validator *CacheControlValidator) map[string]interface{} {
	if len(msg.Content) == 0 {
		return nil
	}

	// Single text part without cache control: use the compact string form
	if len(msg.Content) == 1 && validator == nil {
		if tc, ok := msg.Content[0].(types.TextContent); ok {
			return map[string]interface{}{
				"role":    "user",
				"content": tc.Text,
			}
		}
	}

	// Multi-part or cache control needed: use content array
	parts := make([]map[string]interface{}, 0, len(msg.Content))
	for i, part := range msg.Content {
		var p map[string]interface{}
		isLast := i == len(msg.Content)-1

		switch cp := part.(type) {
		case types.TextContent:
			p = map[string]interface{}{
				"type": "text",
				"text": cp.Text,
			}
		case types.ImageContent:
			var url string
			if cp.URL != "" {
				url = cp.URL
			} else if len(cp.Image) > 0 {
				url = fmt.Sprintf("data:%s;base64,%s",
					cp.MimeType, base64.StdEncoding.EncodeToString(cp.Image))
			}
			p = map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": url},
			}
		default:
			continue
		}

		if validator != nil && isLast {
			cc := validator.GetCacheControl()
			if cc != nil {
				p["cache_control"] = cc
			}
		}

		parts = append(parts, p)
	}

	return map[string]interface{}{
		"role":    "user",
		"content": parts,
	}
}

// convertAssistantMessage converts an assistant message to Alibaba format.
// If validator is non-nil, wraps the text content in a content array to attach the marker.
func convertAssistantMessage(msg types.Message, validator *CacheControlValidator) map[string]interface{} {
	var text string

	for _, part := range msg.Content {
		switch cp := part.(type) {
		case types.TextContent:
			text += cp.Text
		case types.ReasoningContent:
			text += cp.Text
		}
	}

	if validator != nil {
		cc := validator.GetCacheControl()
		part := map[string]interface{}{
			"type": "text",
			"text": text,
		}
		if cc != nil {
			part["cache_control"] = cc
		}
		return map[string]interface{}{
			"role":    "assistant",
			"content": []map[string]interface{}{part},
		}
	}

	return map[string]interface{}{
		"role":    "assistant",
		"content": text,
	}
}

// convertToolMessage converts a tool result message to Alibaba format.
// If validator is non-nil, cache markers are applied to the tool result content.
func convertToolMessage(msg types.Message, validator *CacheControlValidator) map[string]interface{} {
	for _, part := range msg.Content {
		tr, ok := part.(types.ToolResultContent)
		if !ok {
			continue
		}

		contentValue := extractToolResultText(tr)

		if validator != nil {
			cc := validator.GetCacheControl()
			p := map[string]interface{}{
				"type": "text",
				"text": contentValue,
			}
			if cc != nil {
				p["cache_control"] = cc
			}
			return map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tr.ToolCallID,
				"content":      []map[string]interface{}{p},
			}
		}

		return map[string]interface{}{
			"role":         "tool",
			"tool_call_id": tr.ToolCallID,
			"content":      contentValue,
		}
	}
	return nil
}

// extractText extracts all text from a slice of content parts.
func extractText(parts []types.ContentPart) string {
	var text string
	for _, part := range parts {
		if tc, ok := part.(types.TextContent); ok {
			text += tc.Text
		}
	}
	return text
}

// extractToolResultText converts a ToolResultContent to a string for the API.
func extractToolResultText(tr types.ToolResultContent) string {
	if tr.Output != nil {
		switch tr.Output.Type {
		case types.ToolResultOutputText:
			if v, ok := tr.Output.Value.(string); ok {
				return v
			}
		case types.ToolResultOutputJSON, types.ToolResultOutputContent:
			if b, err := json.Marshal(tr.Output.Value); err == nil {
				return string(b)
			}
		case types.ToolResultOutputError:
			if v, ok := tr.Output.Value.(string); ok {
				return v
			}
		}
	}
	// Fallback: use legacy Result field
	if tr.Result != nil {
		if s, ok := tr.Result.(string); ok {
			return s
		}
		if b, err := json.Marshal(tr.Result); err == nil {
			return string(b)
		}
	}
	if tr.Error != "" {
		return tr.Error
	}
	return ""
}
