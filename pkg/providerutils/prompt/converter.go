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
// use as the "content" field of an OpenAI tool role message.
func openAIToolResultText(p types.ToolResultContent) string {
	if p.Output != nil && p.Output.Type == types.ToolResultOutputContent {
		for _, block := range p.Output.Content {
			if textBlock, ok := block.(types.TextContentBlock); ok {
				return textBlock.Text
			}
		}
		return fmt.Sprintf("[complex output from %s]", p.ToolName)
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

// ToGoogleMessages converts unified messages to Google (Gemini) format.
//
// supportsFunctionResponseParts controls whether tool results with image/file
// content are sent using the Gemini 3+ multimodal functionResponse.parts[]
// format (true) or the legacy fallback for older models (false).
func ToGoogleMessages(messages []types.Message, supportsFunctionResponseParts bool) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	for _, msg := range messages {
		switch msg.Role {

		case types.RoleTool:
			// Tool results go as role "user" with functionResponse parts.
			// Each ToolResultContent in the message becomes one functionResponse entry.
			parts := make([]map[string]interface{}, 0, len(msg.Content))
			for _, part := range msg.Content {
				if p, ok := part.(types.ToolResultContent); ok {
					googleAppendFunctionResponse(&parts, p, supportsFunctionResponseParts)
				}
			}
			if len(parts) > 0 {
				result = append(result, map[string]interface{}{
					"role":  "user",
					"parts": parts,
				})
			}

		case types.RoleAssistant:
			// Assistant messages use role "model".
			parts := make([]map[string]interface{}, 0)
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					textPart := map[string]interface{}{"text": p.Text}
					// Restore thoughtSignature from ProviderMetadata when present so
					// Google can verify the reasoning chain on the next turn.
					// Check "google" (Google provider) and "vertex" (Vertex provider) keys.
					if len(p.ProviderMetadata) > 0 {
						var meta map[string]interface{}
						if json.Unmarshal(p.ProviderMetadata, &meta) == nil {
							for _, key := range []string{"google", "vertex", "googleVertex"} {
								if provMeta, ok := meta[key].(map[string]interface{}); ok {
									if sig, ok := provMeta["thoughtSignature"].(string); ok && sig != "" {
										textPart["thoughtSignature"] = sig
										break
									}
								}
							}
						}
					}
					parts = append(parts, textPart)
				case types.ReasoningContent:
					// Emit thought parts with the cryptographic signature Google uses to
					// verify the reasoning chain was not modified across turns.
					// Only emit when there is text or a signature — empty blocks are skipped.
					if p.Text != "" || p.Signature != "" {
						thoughtPart := map[string]interface{}{
							"thought": true,
							"text":    p.Text,
						}
						if p.Signature != "" {
							thoughtPart["thoughtSignature"] = p.Signature
						}
						parts = append(parts, thoughtPart)
					}
				case types.ImageContent:
					imageData := base64.StdEncoding.EncodeToString(p.Image)
					parts = append(parts, map[string]interface{}{
						"inlineData": map[string]interface{}{
							"mimeType": p.MimeType,
							"data":     imageData,
						},
					})
				case types.FileContent:
					parts = append(parts, googleFileContentPart(p))
				case types.CustomContent:
					if googleOpts, ok := p.ProviderOptions["google"].(map[string]interface{}); ok {
						block := map[string]interface{}{}
						for k, v := range googleOpts {
							block[k] = v
						}
						parts = append(parts, block)
					}
				case types.ReasoningFileContent:
					// Reasoning files are not re-sent to Google.
				}
			}
			// Emit functionCall parts for any tool calls the model made.
			// Include ThoughtSignature at part level when present so Google can
			// verify the sealed reasoning chain in multi-turn conversations.
			for _, tc := range msg.ToolCalls {
				functionCall := map[string]interface{}{
					"name": tc.ToolName,
					"args": tc.Arguments,
				}
				if tc.ID != "" {
					functionCall["id"] = tc.ID
				}
				fcPart := map[string]interface{}{
					"functionCall": functionCall,
				}
				if tc.ThoughtSignature != "" {
					fcPart["thoughtSignature"] = tc.ThoughtSignature
				}
				parts = append(parts, fcPart)
			}
			result = append(result, map[string]interface{}{
				"role":  "model",
				"parts": parts,
			})

		default:
			// User (and any other) role → "user".
			parts := make([]map[string]interface{}, 0, len(msg.Content))
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					parts = append(parts, map[string]interface{}{"text": p.Text})
				case types.ImageContent:
					// When a URL is provided use fileData (Cloud Storage / GCS URI).
					// Otherwise send as base64-encoded inlineData.
					if p.URL != "" {
						mimeType := p.MimeType
						if mimeType == "image/*" {
							mimeType = "image/jpeg"
						}
						parts = append(parts, map[string]interface{}{
							"fileData": map[string]interface{}{
								"mimeType": mimeType,
								"fileUri":  p.URL,
							},
						})
					} else {
						imageData := base64.StdEncoding.EncodeToString(p.Image)
						parts = append(parts, map[string]interface{}{
							"inlineData": map[string]interface{}{
								"mimeType": p.MimeType,
								"data":     imageData,
							},
						})
					}
				case types.FileContent:
					parts = append(parts, googleFileContentPart(p))
				case types.CustomContent:
					if googleOpts, ok := p.ProviderOptions["google"].(map[string]interface{}); ok {
						block := map[string]interface{}{}
						for k, v := range googleOpts {
							block[k] = v
						}
						parts = append(parts, block)
					}
				case types.ReasoningFileContent:
					// Reasoning files are not re-sent to Google.
				}
			}
			result = append(result, map[string]interface{}{
				"role":  "user",
				"parts": parts,
			})
		}
	}

	return result
}

// googleAppendFunctionResponse appends a functionResponse part (or parts) for
// a single ToolResultContent to the given parts slice.
func googleAppendFunctionResponse(parts *[]map[string]interface{}, p types.ToolResultContent, supportsFunctionResponseParts bool) {
	if p.Output != nil && p.Output.Type == types.ToolResultOutputContent {
		if supportsFunctionResponseParts {
			googleAppendToolResultParts(parts, p.ToolName, p.Output.Content)
		} else {
			googleAppendLegacyToolResultParts(parts, p.ToolName, p.Output.Content)
		}
		return
	}

	// Simple text/JSON result or error output.
	content := fmt.Sprintf("%v", p.Result)
	if p.Output != nil {
		switch p.Output.Type {
		case types.ToolResultOutputError, types.ToolResultOutputErrorText, types.ToolResultOutputErrorJSON:
			if p.Output.Value != nil {
				content = fmt.Sprintf("%v", p.Output.Value)
			} else {
				content = "Tool execution failed."
			}
		case types.ToolResultOutputExecutionDenied:
			// The tool was blocked by the user approval gate before it ran.
			// Match TS SDK exactly: use reason directly, or default fallback.
			if p.Output.Reason != "" {
				content = p.Output.Reason
			} else {
				content = "Tool execution denied."
			}
		default:
			if p.Output.Value != nil {
				content = fmt.Sprintf("%v", p.Output.Value)
			}
		}
	}
	functionResponse := map[string]interface{}{
		"name": p.ToolName,
		"response": map[string]interface{}{
			"name":    p.ToolName,
			"content": content,
		},
	}
	if p.ToolCallID != "" {
		functionResponse["id"] = p.ToolCallID
	}
	*parts = append(*parts, map[string]interface{}{"functionResponse": functionResponse})
}

// googleAppendToolResultParts implements the Gemini 3+ multimodal
// functionResponse format: text goes into response.content, binary data
// (images/files) go into functionResponse.parts[] as inlineData.
func googleAppendToolResultParts(parts *[]map[string]interface{}, toolName string, blocks []types.ToolResultContentBlock) {
	var textParts []string
	var responseParts []map[string]interface{}

	for _, block := range blocks {
		switch b := block.(type) {
		case types.TextContentBlock:
			textParts = append(textParts, b.Text)
		case types.ImageContentBlock:
			responseParts = append(responseParts, map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": b.MediaType,
					"data":     base64.StdEncoding.EncodeToString(b.Data),
				},
			})
		case types.FileContentBlock:
			responseParts = append(responseParts, googleFileContentBlockPart(b))
		default:
			// Unknown block type — serialize as JSON text.
			if j, err := json.Marshal(block); err == nil {
				textParts = append(textParts, string(j))
			}
		}
	}

	responseContent := "Tool executed successfully."
	if len(textParts) > 0 {
		responseContent = strings.Join(textParts, "\n")
	}

	fr := map[string]interface{}{
		"name": toolName,
		"response": map[string]interface{}{
			"name":    toolName,
			"content": responseContent,
		},
	}
	if len(responseParts) > 0 {
		fr["parts"] = responseParts
	}
	*parts = append(*parts, map[string]interface{}{
		"functionResponse": fr,
	})
}

// googleAppendLegacyToolResultParts implements the pre-Gemini-3 fallback:
// text becomes a plain functionResponse; images become separate top-level
// inlineData parts accompanied by a descriptive text part.
func googleAppendLegacyToolResultParts(parts *[]map[string]interface{}, toolName string, blocks []types.ToolResultContentBlock) {
	for _, block := range blocks {
		switch b := block.(type) {
		case types.TextContentBlock:
			*parts = append(*parts, map[string]interface{}{
				"functionResponse": map[string]interface{}{
					"name": toolName,
					"response": map[string]interface{}{
						"name":    toolName,
						"content": b.Text,
					},
				},
			})
		case types.ImageContentBlock:
			*parts = append(*parts,
				map[string]interface{}{
					"inlineData": map[string]interface{}{
						"mimeType": b.MediaType,
						"data":     base64.StdEncoding.EncodeToString(b.Data),
					},
				},
				map[string]interface{}{
					"text": "Tool executed successfully and returned this image as a response",
				},
			)
		case types.FileContentBlock:
			*parts = append(*parts, googleFileContentBlockPart(b))
		default:
			// Unknown types are serialized to JSON and sent as text.
			j, _ := json.Marshal(block)
			*parts = append(*parts, map[string]interface{}{
				"text": string(j),
			})
		}
	}
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

func googleFileContentPart(file types.FileContent) map[string]interface{} {
	mediaType := firstNonEmpty(file.MediaType, file.MimeType, file.FileData.MediaType)
	if file.URL != "" {
		return map[string]interface{}{
			"fileData": map[string]interface{}{
				"mimeType": mediaType,
				"fileUri":  file.URL,
			},
		}
	}
	if file.Reference != "" {
		return map[string]interface{}{
			"fileData": map[string]interface{}{
				"mimeType": mediaType,
				"fileUri":  file.Reference,
			},
		}
	}
	if file.Text != "" {
		return map[string]interface{}{"text": file.Text}
	}
	return map[string]interface{}{
		"inlineData": map[string]interface{}{
			"mimeType": mediaType,
			"data":     base64.StdEncoding.EncodeToString(file.Data),
		},
	}
}

func googleFileContentBlockPart(block types.FileContentBlock) map[string]interface{} {
	return googleFileContentPart(types.FileContent{
		FileData:        block.FileData,
		Data:            block.Data,
		MediaType:       block.MediaType,
		MimeType:        block.MediaType,
		Filename:        block.Filename,
		URL:             block.URL,
		Reference:       block.Reference,
		Text:            block.Text,
		ProviderOptions: block.ProviderOptions,
	})
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
