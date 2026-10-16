package mistral

import (
	"encoding/base64"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
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
	result, _ := ConvertToMistralChatMessagesWithError(messages)
	return result
}

// ConvertToMistralChatMessagesWithError is ConvertToMistralChatMessages that
// also reports the unsupported-file errors TS throws while converting user
// file parts: provider references, text file parts, and anything other than
// images and PDFs (*errors.UnsupportedFunctionalityError).
func ConvertToMistralChatMessagesWithError(messages []types.Message) ([]map[string]interface{}, error) {
	result := make([]map[string]interface{}, 0, len(messages))
	lastIndex := len(messages) - 1

	for i, msg := range messages {
		isLastMessage := i == lastIndex
		switch msg.Role {
		case types.RoleSystem:
			result = append(result, convertMistralSystemMessage(msg))
		case types.RoleUser:
			converted, err := convertMistralUserMessage(msg)
			if err != nil {
				return result, err
			}
			result = append(result, converted)
		case types.RoleAssistant:
			converted, err := convertMistralAssistantMessage(msg, isLastMessage)
			if err != nil {
				return result, err
			}
			result = append(result, converted)
		case types.RoleTool:
			result = append(result, convertMistralToolMessages(msg)...)
		}
	}

	return result, nil
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
func convertMistralUserMessage(msg types.Message) (map[string]interface{}, error) {
	parts := make([]map[string]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch p := derefMistralUserPart(part).(type) {
		case types.TextContent:
			parts = append(parts, map[string]interface{}{"type": "text", "text": p.Text})
		case types.ImageContent:
			fc, err := prompt.NormalizeImageContent(p)
			if err != nil {
				return nil, err
			}
			converted, err := convertMistralFilePart(fc)
			if err != nil {
				return nil, err
			}
			parts = append(parts, converted)
		case types.FileContent:
			converted, err := convertMistralFilePart(p)
			if err != nil {
				return nil, err
			}
			parts = append(parts, converted)
		}
	}
	return map[string]interface{}{"role": "user", "content": parts}, nil
}

// convertMistralFilePart mirrors the TS user file-part switch: images become
// image_url, PDFs document_url; references, text parts and other media types
// are unsupported.
func convertMistralFilePart(fc types.FileContent) (map[string]interface{}, error) {
	n, err := prompt.NormalizeFileContent(fc)
	if err != nil {
		return nil, err
	}
	switch n.FileData.Type {
	case types.FileDataTypeReference:
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "file parts with provider references"}
	case types.FileDataTypeText:
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "text file parts"}
	}
	mediaType := n.MediaType
	isData := n.FileData.Type == types.FileDataTypeData
	// TS formatFileUrl: urls pass through; inline bytes embed the media type
	// resolved by resolveFullMediaType (signature-sniffed, error if unknown).
	formatURL := func() (string, error) {
		if !isData {
			return n.FileData.URL, nil
		}
		full, err := prompt.ResolveFullMediaType(mediaType, n.FileData.Data, true)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("data:%s;base64,%s", full, base64.StdEncoding.EncodeToString(n.FileData.Data)), nil
	}
	if mediaType == "image" || strings.HasPrefix(mediaType, "image/") {
		u, err := formatURL()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"type": "image_url", "image_url": u}, nil
	}
	checked := mediaType
	if isData {
		full, err := prompt.ResolveFullMediaType(mediaType, n.FileData.Data, true)
		if err != nil {
			return nil, err
		}
		checked = full
	}
	if checked != "application/pdf" {
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "Only images and PDF file parts are supported"}
	}
	u, err := formatURL()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"type": "document_url", "document_url": u}, nil
}

// convertMistralAssistantMessage converts an assistant message. content is a
// plain string of the concatenated text unless the message carries any
// reasoning, in which case content becomes an ordered array mixing
// {"type":"text",...} and {"type":"thinking",...} blocks (mirrors TS exactly:
// `content: hasReasoning ? contentParts : text`).
func convertMistralAssistantMessage(msg types.Message, isLastMessage bool) (map[string]interface{}, error) {
	var text strings.Builder
	hasReasoning := false
	var contentParts []map[string]interface{}

	for _, part := range msg.Content {
		switch p := derefMistralPart(part).(type) {
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
		case types.ToolCallContent, types.SourceContent, types.ToolApprovalRequestContent, types.ToolErrorContent:
			// Tool calls are collected below. Sources, approval requests and
			// tool errors are SDK-side parts that TS strips before the
			// provider sees the prompt.
		default:
			// TS: throw new Error(`Unsupported content type in assistant message: ${part.type}`)
			return nil, fmt.Errorf("Unsupported content type in assistant message: %s", part.ContentType()) //nolint:staticcheck // mirrors TS error text
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
	// TS-shaped history carries tool calls as content parts; Message.ToolCalls
	// is the older Go shape.
	toolCalls := msg.ToolCalls
	if len(toolCalls) == 0 {
		for _, part := range msg.Content {
			if tc, ok := derefMistralPart(part).(types.ToolCallContent); ok {
				toolCalls = append(toolCalls, types.ToolCall{ID: tc.ToolCallID, ToolName: tc.ToolName, Arguments: tc.Arguments, RawArguments: tc.Input})
			}
		}
	}
	if len(toolCalls) > 0 {
		result["tool_calls"] = mistralToolCalls(toolCalls)
	}
	return result, nil
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
			if b, err := jsonstr.Marshal(args); err == nil {
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
		var tr types.ToolResultContent
		switch p := part.(type) {
		case types.ToolResultContent:
			tr = p
		case *types.ToolResultContent:
			if p == nil {
				continue
			}
			tr = *p
		default:
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
	return prompt.ToolResultText(tr)
}

// derefMistralPart unwraps pointer content parts so they match the value
// cases of the assistant part switch.
func derefMistralPart(part types.ContentPart) types.ContentPart {
	switch p := part.(type) {
	case *types.TextContent:
		if p != nil {
			return *p
		}
	case *types.ReasoningContent:
		if p != nil {
			return *p
		}
	case *types.ToolCallContent:
		if p != nil {
			return *p
		}
	case *types.SourceContent:
		if p != nil {
			return *p
		}
	case *types.ToolApprovalRequestContent:
		if p != nil {
			return *p
		}
	case *types.ToolErrorContent:
		if p != nil {
			return *p
		}
	}
	return part
}

// derefMistralUserPart unwraps pointer user content parts so a *TextContent or
// *FileContent is converted exactly like its value form instead of dropped.
func derefMistralUserPart(part types.ContentPart) types.ContentPart {
	switch p := part.(type) {
	case *types.TextContent:
		if p != nil {
			return *p
		}
	case *types.ImageContent:
		if p != nil {
			return *p
		}
	case *types.FileContent:
		if p != nil {
			return *p
		}
	}
	return part
}
