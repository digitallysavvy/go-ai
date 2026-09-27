package prompt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToOpenAIMessagesOptions controls provider-specific variations in
// ToOpenAIMessages behavior. The zero value matches the behavior of every
// non-OpenAI TS consumer of this wire format (Groq, DeepSeek,
// @ai-sdk/openai-compatible's Together/Fireworks/Mistral/Ollama/..., Alibaba):
// they all just JSON.stringify(part.input) with no sanitization.
type ToOpenAIMessagesOptions struct {
	// SanitizeReplayedToolCallArguments matches OpenAI's own
	// serializeToolCallArguments (packages/openai/src/chat/convert-to-openai-chat-messages.ts):
	// a replayed RawArguments string that doesn't parse to a JSON object is
	// sent as "{}" rather than forwarded verbatim. This sanitization is
	// unique to OpenAI's chat-completions conversion in the TS SDK -- every
	// other provider that shares this OpenAI-shaped wire format
	// (Groq/DeepSeek/openai-compatible/Alibaba) does not do this, so callers
	// other than OpenAI's and Azure's own chat-completions models (Azure's
	// `chat()` factory wraps OpenAIChatLanguageModel in TS) must leave this
	// false.
	SanitizeReplayedToolCallArguments bool
}

// ToOpenAIMessages converts unified messages to OpenAI Chat Completions format.
//
// Key invariants maintained:
//   - Assistant messages that contain tool calls emit a top-level "tool_calls"
//     array, which OpenAI requires to be present before any "tool" role messages.
//   - Tool role messages emit "tool_call_id" as a top-level field and their
//     result as a plain string in "content" — the format OpenAI expects.
//
// opts is variadic so existing call sites are unaffected; at most the first
// element is used.
func ToOpenAIMessages(messages []types.Message, opts ...ToOpenAIMessagesOptions) []map[string]interface{} {
	var opt ToOpenAIMessagesOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	result := make([]map[string]interface{}, 0, len(messages))

	for _, msg := range messages {
		// ── Tool role messages ────────────────────────────────────────────────
		// OpenAI requires: {"role":"tool","tool_call_id":"...","content":"..."}
		// tool_call_id and content are both top-level; there is no content array.
		if msg.Role == types.RoleTool {
			for _, part := range msg.Content {
				if p, ok := part.(types.ToolResultContent); ok {
					result = append(result, map[string]interface{}{
						"role":         "tool",
						"tool_call_id": p.ToolCallID,
						"content":      openAIToolResultText(p),
					})
				}
			}
			continue
		}

		// ── All other roles ───────────────────────────────────────────────────
		openAIMsg := map[string]interface{}{
			"role": string(msg.Role),
		}

		// Assistant messages that made tool calls must carry the tool_calls array
		// so that the subsequent tool role messages are considered valid by OpenAI.
		if msg.Role == types.RoleAssistant && len(msg.ToolCalls) > 0 {
			toolCalls := openAIToolCalls(msg.ToolCalls, opt.SanitizeReplayedToolCallArguments)
			openAIMsg["tool_calls"] = toolCalls
			text := assistantTextContent(msg.Content)
			if text == "" {
				openAIMsg["content"] = nil
			} else {
				openAIMsg["content"] = text
			}
			if msg.Name != "" {
				openAIMsg["name"] = msg.Name
			}
			result = append(result, openAIMsg)
			continue
		}

		// Handle content parts
		if len(msg.Content) == 1 && msg.Content[0].ContentType() == "text" {
			if textContent, ok := msg.Content[0].(types.TextContent); ok {
				openAIMsg["content"] = textContent.Text
			}
		} else if len(msg.Content) > 0 {
			contentParts := make([]map[string]interface{}, 0, len(msg.Content))
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					contentParts = append(contentParts, map[string]interface{}{
						"type": "text",
						"text": p.Text,
					})
				case types.ImageContent:
					var imageData string
					if p.URL != "" {
						imageData = p.URL
					} else {
						imageData = fmt.Sprintf("data:%s;base64,%s",
							p.MimeType, base64.StdEncoding.EncodeToString(p.Image))
					}
					imageURL := map[string]interface{}{
						"url": imageData,
					}
					if detail := openAIImageDetail(p.ProviderOptions); detail != "" {
						imageURL["detail"] = detail
					}
					contentParts = append(contentParts, map[string]interface{}{
						"type":      "image_url",
						"image_url": imageURL,
					})
				case types.FileContent:
					contentParts = append(contentParts, openAIFileContentPart(p))
				case types.CustomContent:
					// CustomContent in assistant messages may carry OpenAI-specific
					// provider options. Forward the openai-keyed options verbatim if
					// present; otherwise skip.
					if openaiOpts, ok := p.ProviderOptions["openai"].(map[string]interface{}); ok {
						block := map[string]interface{}{}
						for k, v := range openaiOpts {
							block[k] = v
						}
						contentParts = append(contentParts, block)
					}
				case types.ReasoningFileContent:
					// Reasoning files are not re-sent to OpenAI.
				}
			}
			if len(contentParts) > 0 {
				openAIMsg["content"] = contentParts
			}
		} else if msg.Role == types.RoleAssistant {
			openAIMsg["content"] = ""
		}

		if msg.Name != "" {
			openAIMsg["name"] = msg.Name
		}

		result = append(result, openAIMsg)
	}

	return result
}

func openAIToolCalls(toolCalls []types.ToolCall, sanitizeReplayedArguments bool) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		arguments := tc.RawArguments
		if arguments != "" {
			// 2523403 (OpenAI only -- see ToOpenAIMessagesOptions): a replayed
			// RawArguments string that doesn't parse to a JSON object (e.g. an
			// array, string, number, or invalid JSON) is sent as "{}" instead
			// of forwarded verbatim.
			if sanitizeReplayedArguments {
				var probe interface{}
				if err := json.Unmarshal([]byte(arguments), &probe); err != nil {
					arguments = "{}"
				} else if _, isObject := probe.(map[string]interface{}); !isObject {
					arguments = "{}"
				}
			}
		} else {
			args := tc.Arguments
			if args == nil {
				args = map[string]interface{}{}
			}
			argsJSON, _ := json.Marshal(args)
			arguments = string(argsJSON)
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

func assistantTextContent(content []types.ContentPart) string {
	var b strings.Builder
	for _, part := range content {
		if text, ok := part.(types.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// openAIToolResultText extracts a plain string from a ToolResultContent for
// use as the "content" field of an OpenAI tool role message, mirroring TS
// convert-to-openai-chat-messages.ts's per-output-type switch: text/error-text
// use the raw string value, execution-denied falls back to the default denial
// text (audit row 58a2ad7 / G6), and json/error-json are JSON-stringified.
// Reuses resolveToolOutput (shared with the Anthropic converter) so the
// legacy Result/Error fields are also handled. Before this fix, every output
// other than "content" fell through to the deprecated Result field, which
// resolveToolOutput leaves nil once Output is set, printing the literal
// string "<nil>" as the tool's content.
func openAIToolResultText(p types.ToolResultContent) string {
	out := resolveToolOutput(p)
	switch out.kind {
	case "text", "error-text":
		return stringValue(out.value)
	case "execution-denied":
		if out.reason != "" {
			return out.reason
		}
		return "Tool call execution denied."
	case "content":
		for _, block := range out.content {
			if textBlock, ok := block.(types.TextContentBlock); ok {
				return textBlock.Text
			}
		}
		return fmt.Sprintf("[complex output from %s]", p.ToolName)
	default: // json, error-json
		return jsonStringify(out.value)
	}
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

func openAIFileContentPart(file types.FileContent) map[string]interface{} {
	mediaType := firstNonEmpty(file.MediaType, file.MimeType, file.FileData.MediaType)
	if strings.HasPrefix(mediaType, "image/") || mediaType == "image" {
		imageURL := file.URL
		if imageURL == "" && len(file.Data) > 0 {
			imageURL = fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(file.Data))
		}
		image := map[string]interface{}{
			"url": imageURL,
		}
		if detail := openAIImageDetail(file.ProviderOptions); detail != "" {
			image["detail"] = detail
		}
		return map[string]interface{}{
			"type":      "image_url",
			"image_url": image,
		}
	}

	fileObject := map[string]interface{}{}
	if file.Filename != "" {
		fileObject["filename"] = file.Filename
	}
	switch {
	case file.Reference != "":
		fileObject["file_id"] = file.Reference
	case file.URL != "":
		fileObject["file_url"] = file.URL
	case file.Text != "":
		fileObject["file_data"] = file.Text
	case len(file.Data) > 0:
		fileObject["file_data"] = base64.StdEncoding.EncodeToString(file.Data)
	}
	if mediaType != "" {
		fileObject["media_type"] = mediaType
	}
	return map[string]interface{}{
		"type": "file",
		"file": fileObject,
	}
}

func openAIImageDetail(providerOptions map[string]interface{}) string {
	if providerOptions == nil {
		return ""
	}
	openaiOpts, ok := providerOptions["openai"].(map[string]interface{})
	if !ok {
		return ""
	}
	if detail, ok := openaiOpts["imageDetail"].(string); ok {
		return detail
	}
	if detail, ok := openaiOpts["detail"].(string); ok {
		return detail
	}
	return ""
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
