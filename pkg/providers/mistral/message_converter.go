package mistral

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertToMistralChatMessages converts unified SDK messages into Mistral's
// chat completion wire format, mirroring TS
// convert-to-mistral-chat-messages.ts. It differs from the generic
// OpenAI-compatible converter (prompt.ToOpenAIMessages) in several
// Mistral-specific ways:
//
//   - User file parts: images are sent as {"type":"image_url","image_url":<url-string>}
//     (a bare URL string, not an {"url": ...} object); PDFs are sent as
//     {"type":"document_url","document_url":<url-string>}.
//   - Assistant reasoning: types.ReasoningContent parts are emitted as
//     {"type":"thinking","thinking":[{"type":"text","text":...}],"closed":true}
//     content blocks. When any reasoning is present, the assistant message's
//     "content" becomes an array of blocks instead of a plain string, so the
//     reasoning survives round-tripping through multi-turn conversation
//     history (it is otherwise dropped by prompt.ToOpenAIMessages).
//   - Assistant "prefix": the last message in the prompt gets "prefix": true
//     when it is an assistant message, matching Mistral's
//     continuation/prefill mechanism.
//   - Tool results are stringified per Mistral's rules (JSON.stringify for
//     content/json outputs, the reason text for execution-denied, etc.).
func ConvertToMistralChatMessages(messages []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))
	lastIndex := len(messages) - 1

	for i, msg := range messages {
		isLastMessage := i == lastIndex
		switch msg.Role {
		case types.RoleSystem:
			result = append(result, convertMistralSystemMessage(msg))
		case types.RoleUser:
			result = append(result, convertMistralUserMessage(msg))
		case types.RoleAssistant:
			result = append(result, convertMistralAssistantMessage(msg, isLastMessage))
		case types.RoleTool:
			result = append(result, convertMistralToolMessages(msg)...)
		}
	}

	return result
}

// convertMistralSystemMessage converts a system message. A single text part
// is sent as a plain string (Mistral/TS's usual shape); multiple parts (only
// possible via Go's unified Message, since TS system content is always a
// string) are sent as a content-part array so no text is lost.
func convertMistralSystemMessage(msg types.Message) map[string]interface{} {
	if len(msg.Content) == 1 {
		if tc, ok := msg.Content[0].(types.TextContent); ok {
			return map[string]interface{}{"role": "system", "content": tc.Text}
		}
	}
	parts := make([]map[string]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		if tc, ok := part.(types.TextContent); ok {
			parts = append(parts, map[string]interface{}{"type": "text", "text": tc.Text})
		}
	}
	return map[string]interface{}{"role": "system", "content": parts}
}

// convertMistralUserMessage converts a user message. Content is always an
// array of parts, matching TS (`content: content.map(...)`).
func convertMistralUserMessage(msg types.Message) map[string]interface{} {
	parts := make([]map[string]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			parts = append(parts, map[string]interface{}{"type": "text", "text": p.Text})
		case types.ImageContent:
			parts = append(parts, map[string]interface{}{
				"type":      "image_url",
				"image_url": mistralFileURL(p.URL, p.MimeType, p.Image),
			})
		case types.FileContent:
			mediaType := firstNonEmptyMistral(p.MediaType, p.MimeType, p.FileData.MediaType)
			url := mistralFileURL(firstNonEmptyMistral(p.URL, p.Reference), mediaType, p.Data)
			if strings.HasPrefix(mediaType, "image/") {
				parts = append(parts, map[string]interface{}{"type": "image_url", "image_url": url})
			} else {
				parts = append(parts, map[string]interface{}{"type": "document_url", "document_url": url})
			}
		}
	}
	return map[string]interface{}{"role": "user", "content": parts}
}

// convertMistralAssistantMessage converts an assistant message. content is a
// plain string of the concatenated text unless the message carries any
// reasoning, in which case content becomes an ordered array mixing
// {"type":"text",...} and {"type":"thinking",...} blocks (mirrors TS exactly:
// `content: hasReasoning ? contentParts : text`).
func convertMistralAssistantMessage(msg types.Message, isLastMessage bool) map[string]interface{} {
	var text strings.Builder
	hasReasoning := false
	var contentParts []map[string]interface{}

	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			text.WriteString(p.Text)
			contentParts = append(contentParts, map[string]interface{}{"type": "text", "text": p.Text})
		case types.ReasoningContent:
			hasReasoning = true
			contentParts = append(contentParts, map[string]interface{}{
				"type": "thinking",
				"thinking": []map[string]interface{}{
					{"type": "text", "text": p.Text},
				},
				"closed": true,
			})
		}
	}

	result := map[string]interface{}{"role": "assistant"}
	if hasReasoning {
		result["content"] = contentParts
	} else {
		result["content"] = text.String()
	}
	if isLastMessage {
		result["prefix"] = true
	}
	if len(msg.ToolCalls) > 0 {
		result["tool_calls"] = mistralToolCalls(msg.ToolCalls)
	}
	return result
}

// mistralToolCalls builds the OpenAI-shaped tool_calls array Mistral expects
// in assistant message history.
func mistralToolCalls(toolCalls []types.ToolCall) []map[string]interface{} {
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

// convertMistralToolMessages converts a tool-role message into one Mistral
// tool message per ToolResultContent part (mirrors TS, which emits one
// message per toolResponse). tool-approval-response parts are skipped, as in
// TS.
func convertMistralToolMessages(msg types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		tr, ok := part.(types.ToolResultContent)
		if !ok {
			continue
		}
		result = append(result, map[string]interface{}{
			"role":         "tool",
			"name":         tr.ToolName,
			"tool_call_id": tr.ToolCallID,
			"content":      mistralToolResultText(tr),
		})
	}
	return result
}

// mistralToolResultText stringifies a tool result the way Mistral expects:
// text/error-text outputs pass the value through as-is, execution-denied
// uses the reason (or a default), and content/json/error-json outputs are
// JSON-stringified. Falls back to the legacy Result/Error fields when Output
// is unset.
func mistralToolResultText(tr types.ToolResultContent) string {
	if tr.Output != nil {
		switch tr.Output.Type {
		case types.ToolResultOutputText, types.ToolResultOutputErrorText:
			if v, ok := tr.Output.Value.(string); ok {
				return v
			}
			return fmt.Sprintf("%v", tr.Output.Value)
		case types.ToolResultOutputExecutionDenied:
			if tr.Output.Reason != "" {
				return tr.Output.Reason
			}
			return "Tool call execution denied."
		case types.ToolResultOutputContent, types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON:
			if b, err := json.Marshal(tr.Output.Value); err == nil {
				return string(b)
			}
		}
	}
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

// mistralFileURL returns the URL to send for a file/image part: the URL
// as-is when present, otherwise a base64 data: URL built from raw bytes.
func mistralFileURL(url, mediaType string, data []byte) string {
	if url != "" {
		return url
	}
	return fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(data))
}

// firstNonEmptyMistral returns the first non-empty string argument.
func firstNonEmptyMistral(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
