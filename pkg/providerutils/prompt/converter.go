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

	// IncludePromptCacheBreakpoint forwards a per-part
	// providerOptions.openai.promptCacheBreakpoint value as a
	// "prompt_cache_breakpoint" field on the emitted wire part (b2b1bb9,
	// convert-to-openai-chat-messages.ts's getPromptCacheBreakpoint). This is
	// unique to OpenAI's own chat-completions conversion in the TS SDK (the
	// shared @ai-sdk/openai-compatible base used by Together/Fireworks/
	// Mistral/Ollama/etc. has no such option) -- callers other than OpenAI's
	// and Azure's chat-completions models must leave this false.
	IncludePromptCacheBreakpoint bool
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
					toolMsg := map[string]interface{}{
						"role":         "tool",
						"tool_call_id": p.ToolCallID,
					}
					text := openAIToolResultText(p)
					if opt.IncludePromptCacheBreakpoint {
						if bp, ok := openAIToolResultPromptCacheBreakpoint(p); ok {
							toolMsg["content"] = []map[string]interface{}{
								{"type": "text", "text": text, "prompt_cache_breakpoint": bp},
							}
							result = append(result, toolMsg)
							continue
						}
					}
					toolMsg["content"] = text
					result = append(result, toolMsg)
				}
			}
			continue
		}

		// ── System role messages ────────────────────────────────────────────────
		// TS convertToOpenAIChatMessages's system/developer case: system message
		// content is a plain string (no per-part providerOptions in TS's core
		// types), so the promptCacheBreakpoint carrier is the MESSAGE's own
		// providerOptions field (types.Message.ProviderOptions), not a content
		// part's -- unlike user/assistant/tool messages. Only forwarded when
		// IncludePromptCacheBreakpoint is set (OpenAI/Azure chat only); other
		// ToOpenAIMessages callers keep emitting a plain string via the generic
		// path below, matching @ai-sdk/openai-compatible's system case, which has
		// no such option.
		if opt.IncludePromptCacheBreakpoint && msg.Role == types.RoleSystem {
			text := assistantTextContent(msg.Content)
			systemMsg := map[string]interface{}{"role": string(msg.Role)}
			if bp, ok := openAIPromptCacheBreakpoint(msg.ProviderOptions); ok {
				systemMsg["content"] = []map[string]interface{}{
					{"type": "text", "text": text, "prompt_cache_breakpoint": bp},
				}
			} else {
				systemMsg["content"] = text
			}
			result = append(result, systemMsg)
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
			if opt.IncludePromptCacheBreakpoint {
				if textParts, hasBreakpoint := assistantTextPartsWithBreakpoint(msg.Content); hasBreakpoint {
					openAIMsg["content"] = textParts
					if msg.Name != "" {
						openAIMsg["name"] = msg.Name
					}
					result = append(result, openAIMsg)
					continue
				}
			}
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

		// Handle content parts. The single-text fast path is skipped when
		// IncludePromptCacheBreakpoint is set and that lone part carries a
		// breakpoint -- TS convertToOpenAIChatMessages only takes the plain
		// string shortcut when getPromptCacheBreakpoint(content[0].providerOptions) == null.
		useSingleTextShortcut := len(msg.Content) == 1 && msg.Content[0].ContentType() == "text"
		if useSingleTextShortcut && opt.IncludePromptCacheBreakpoint {
			if textContent, ok := msg.Content[0].(types.TextContent); ok {
				if _, has := openAIPromptCacheBreakpoint(textContent.ProviderOptions); has {
					useSingleTextShortcut = false
				}
			}
		}
		if useSingleTextShortcut {
			if textContent, ok := msg.Content[0].(types.TextContent); ok {
				openAIMsg["content"] = textContent.Text
			}
		} else if len(msg.Content) > 0 {
			contentParts := make([]map[string]interface{}, 0, len(msg.Content))
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					textPart := map[string]interface{}{
						"type": "text",
						"text": p.Text,
					}
					if opt.IncludePromptCacheBreakpoint {
						if bp, ok := openAIPromptCacheBreakpoint(p.ProviderOptions); ok {
							textPart["prompt_cache_breakpoint"] = bp
						}
					}
					contentParts = append(contentParts, textPart)
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
					imagePart := map[string]interface{}{
						"type":      "image_url",
						"image_url": imageURL,
					}
					if opt.IncludePromptCacheBreakpoint {
						if bp, ok := openAIPromptCacheBreakpoint(p.ProviderOptions); ok {
							imagePart["prompt_cache_breakpoint"] = bp
						}
					}
					contentParts = append(contentParts, imagePart)
				case types.FileContent:
					filePart := openAIFileContentPart(p)
					if opt.IncludePromptCacheBreakpoint {
						if bp, ok := openAIPromptCacheBreakpoint(p.ProviderOptions); ok {
							filePart["prompt_cache_breakpoint"] = bp
						}
					}
					contentParts = append(contentParts, filePart)
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

// assistantTextPartsWithBreakpoint builds the per-part "text" array TS emits
// for an assistant message's content when any text part carries a
// providerOptions.openai.promptCacheBreakpoint (convert-to-openai-chat-messages.ts's
// assistant case: `content: hasPromptCacheBreakpoint ? textParts : ...`).
// It always includes every text part -- not just the one(s) with a
// breakpoint -- once any single part triggers the array form. The second
// return value reports whether any part actually had a breakpoint.
func assistantTextPartsWithBreakpoint(content []types.ContentPart) ([]map[string]interface{}, bool) {
	textParts := make([]map[string]interface{}, 0, len(content))
	hasBreakpoint := false
	for _, part := range content {
		text, ok := part.(types.TextContent)
		if !ok {
			continue
		}
		textPart := map[string]interface{}{
			"type": "text",
			"text": text.Text,
		}
		if bp, ok := openAIPromptCacheBreakpoint(text.ProviderOptions); ok {
			textPart["prompt_cache_breakpoint"] = bp
			hasBreakpoint = true
		}
		textParts = append(textParts, textPart)
	}
	if !hasBreakpoint {
		return nil, false
	}
	return textParts, true
}

// openAIPromptCacheBreakpoint extracts providerOptions.openai.promptCacheBreakpoint
// verbatim (TS getPromptCacheBreakpoint, convert-to-openai-chat-messages.ts).
// The value is forwarded as-is -- TS types it as `{ mode: 'explicit' }` but
// never inspects its shape, only whether it is present.
func openAIPromptCacheBreakpoint(providerOptions map[string]interface{}) (interface{}, bool) {
	if providerOptions == nil {
		return nil, false
	}
	openaiOpts, ok := providerOptions["openai"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	bp, ok := openaiOpts["promptCacheBreakpoint"]
	if !ok || bp == nil {
		return nil, false
	}
	return bp, true
}

// toolResultContentBlockProviderOptions extracts ProviderOptions from a tool
// result content block, if the concrete block type carries one.
func toolResultContentBlockProviderOptions(block types.ToolResultContentBlock) map[string]interface{} {
	switch b := block.(type) {
	case types.TextContentBlock:
		return b.ProviderOptions
	case types.ImageContentBlock:
		return b.ProviderOptions
	case types.FileContentBlock:
		return b.ProviderOptions
	case types.CustomContentBlock:
		return b.ProviderOptions
	default:
		return nil
	}
}

// openAIToolResultPromptCacheBreakpoint mirrors TS's tool-response
// promptCacheBreakpoint resolution (convert-to-openai-chat-messages.ts):
// for a "content" output, the first content-block breakpoint found wins;
// otherwise the output's own providerOptions; falling back to the tool
// response part's own providerOptions.
func openAIToolResultPromptCacheBreakpoint(p types.ToolResultContent) (interface{}, bool) {
	if p.Output != nil {
		if p.Output.Type == types.ToolResultOutputContent {
			for _, block := range p.Output.Content {
				if bp, ok := openAIPromptCacheBreakpoint(toolResultContentBlockProviderOptions(block)); ok {
					return bp, true
				}
			}
		} else if bp, ok := openAIPromptCacheBreakpoint(p.Output.ProviderOptions); ok {
			return bp, true
		}
	}
	return openAIPromptCacheBreakpoint(p.ProviderOptions)
}

// openAIToolResultText extracts a plain string from a ToolResultContent for
// use as the "content" field of an OpenAI tool role message. Mirrors TS's
// tool-response contentValue switch exactly (convert-to-openai-chat-messages.ts,
// and identically in @ai-sdk/openai-compatible's convert-to-openai-compatible-
// chat-messages.ts, so this applies to every ToOpenAIMessages caller): text
// and error-text forward the value verbatim, execution-denied uses the
// denial reason (or a default message), and content/json/error-json all
// JSON.stringify the output's value -- "content" is JSON.stringify(the whole
// block array), not a first-text-block extraction. Falls back to the legacy
// Result field when Output isn't set.
func openAIToolResultText(p types.ToolResultContent) string {
	if p.Output != nil {
		switch p.Output.Type {
		case types.ToolResultOutputText, types.ToolResultOutputErrorText:
			if s, ok := p.Output.Value.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", p.Output.Value)
		case types.ToolResultOutputExecutionDenied:
			if p.Output.Reason != "" {
				return p.Output.Reason
			}
			return "Tool call execution denied."
		case types.ToolResultOutputContent:
			// TS's contentValue switch JSON.stringifies output.value (the
			// whole content-block array) for the "content" case, exactly
			// like "json"/"error-json" -- both OpenAI's own converter and
			// the shared @ai-sdk/openai-compatible base do this, there is no
			// first-text-block extraction in either. Mirror ToolResultOutput.
			// MarshalJSON's own value/Content precedence so a round-tripped
			// (Value set) and a natively-built (Content set) output produce
			// the same wire text.
			var value interface{} = p.Output.Content
			if p.Output.Value != nil {
				value = p.Output.Value
			}
			if value == nil {
				value = []types.ToolResultContentBlock{}
			}
			if b, err := json.Marshal(value); err == nil {
				return string(b)
			}
			return fmt.Sprintf("[complex output from %s]", p.ToolName)
		case types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON:
			if b, err := json.Marshal(p.Output.Value); err == nil {
				return string(b)
			}
			return fmt.Sprintf("%v", p.Output.Value)
		}
	}
	return fmt.Sprintf("%v", p.Result)
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

	// Audio content parts use OpenAI's dedicated input_audio wire shape
	// rather than the generic "file" shape (TS convert-to-openai-chat-messages.ts):
	// only inline data is supported (audio URLs aren't), and only wav/mp3.
	if len(file.Data) > 0 {
		var format string
		switch mediaType {
		case "audio/wav":
			format = "wav"
		case "audio/mp3", "audio/mpeg":
			format = "mp3"
		}
		if format != "" {
			return map[string]interface{}{
				"type": "input_audio",
				"input_audio": map[string]interface{}{
					"data":   base64.StdEncoding.EncodeToString(file.Data),
					"format": format,
				},
			}
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
