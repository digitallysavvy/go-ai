package alibaba

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertToAlibabaChatMessages converts SDK messages to Alibaba API format.
//
// Alibaba uses an OpenAI-compatible message format. When validator is non-nil,
// cache markers are applied to the last eligible content part of each message,
// enabling prompt caching for system, user, assistant, and tool messages.
// The validator enforces the 4-breakpoint limit and accumulates warnings.
//
// preserveThinking controls whether assistant reasoning from turns before the
// current one is replayed as reasoning_content. Reasoning from the current
// round (any assistant message after the last user message) is always
// included, since it always accompanies tool calls; earlier rounds are only
// replayed when preserveThinking is true (mirrors TS
// convertToAlibabaChatMessages / supportsPreservedThinking gating done by the
// caller).
//
// Pass nil for validator to produce compact string content with no caching.
func ConvertToAlibabaChatMessages(messages []types.Message, validator *CacheControlValidator, preserveThinking bool) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	lastUserMessageIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleUser {
			lastUserMessageIndex = i
			break
		}
	}

	for i, msg := range messages {
		includeReasoning := preserveThinking || i > lastUserMessageIndex
		converted := convertMessage(msg, validator, includeReasoning)
		if converted != nil {
			result = append(result, converted)
		}
	}

	return result
}

// convertMessage converts a single message to Alibaba API format.
func convertMessage(msg types.Message, validator *CacheControlValidator, includeReasoning bool) map[string]interface{} {
	switch msg.Role {
	case types.RoleSystem:
		return convertSystemMessage(msg, validator)
	case types.RoleUser:
		return convertUserMessage(msg, validator)
	case types.RoleAssistant:
		return convertAssistantMessage(msg, validator, includeReasoning)
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
// If validator is non-nil, wraps the text content in a content array to
// attach the marker. Reasoning is emitted as a separate reasoning_content
// field (never inlined into content) and is only included when
// includeReasoning is true, matching TS convertToAlibabaChatMessages. Tool
// calls (types.Message.ToolCalls) are emitted as an OpenAI-shaped tool_calls
// array.
func convertAssistantMessage(msg types.Message, validator *CacheControlValidator, includeReasoning bool) map[string]interface{} {
	var text strings.Builder
	var reasoning strings.Builder

	for _, part := range msg.Content {
		switch cp := part.(type) {
		case types.TextContent:
			text.WriteString(cp.Text)
		case types.ReasoningContent:
			if includeReasoning {
				reasoning.WriteString(cp.Text)
			}
		}
	}

	textStr := text.String()
	reasoningStr := reasoning.String()
	hasToolCalls := len(msg.ToolCalls) > 0

	// A pure-reasoning assistant turn (e.g. a preserved earlier round with no
	// visible text and no tool calls) is dropped entirely, matching TS's
	// `break` when text/toolCalls/reasoningContent are all empty.
	if textStr == "" && !hasToolCalls && reasoningStr == "" {
		return nil
	}

	result := map[string]interface{}{"role": "assistant"}

	var cacheControl *MessageCacheControl
	if validator != nil {
		cacheControl = validator.GetCacheControl()
	}

	switch {
	case textStr == "" && !hasToolCalls && reasoningStr != "":
		// Reasoning-only turn: content is explicitly null.
		result["content"] = nil
	case cacheControl != nil:
		result["content"] = []map[string]interface{}{{
			"type":          "text",
			"text":          textStr,
			"cache_control": cacheControl,
		}}
	case textStr == "":
		result["content"] = nil
	default:
		result["content"] = textStr
	}

	if reasoningStr != "" {
		result["reasoning_content"] = reasoningStr
	}
	if hasToolCalls {
		result["tool_calls"] = alibabaOpenAIStyleToolCalls(msg.ToolCalls)
	}

	return result
}

// alibabaOpenAIStyleToolCalls builds the OpenAI-shaped tool_calls array Alibaba
// expects in assistant message history.
func alibabaOpenAIStyleToolCalls(toolCalls []types.ToolCall) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		arguments := tc.RawArguments
		if arguments == "" {
			args := tc.Arguments
			if args == nil {
				args = map[string]interface{}{}
			}
			if b, err := json.Marshal(args); err == nil {
				arguments = string(b)
			}
		}
		result = append(result, map[string]interface{}{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      tc.ToolName,
				"arguments": arguments,
			},
		})
	}
	return result
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
		case types.ToolResultOutputError, types.ToolResultOutputErrorText:
			if v, ok := tr.Output.Value.(string); ok {
				return v
			}
		case types.ToolResultOutputErrorJSON:
			if b, err := json.Marshal(tr.Output.Value); err == nil {
				return string(b)
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
