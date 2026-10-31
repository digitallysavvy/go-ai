package alibaba

import (
	"encoding/base64"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// ConvertToAlibabaChatMessagesWithError mirrors TS convertToAlibabaChatMessages:
// cache markers come from providerOptions.alibaba.cacheControl on a message, a
// user content part or a tool result (validator must be a per-part validator,
// or nil for no caching), unsupported file parts fail with an
// UnsupportedFunctionalityError, and every tool result becomes its own tool
// message.
func ConvertToAlibabaChatMessagesWithError(messages []types.Message, validator *CacheControlValidator, preserveThinking bool) ([]map[string]interface{}, error) {
	cacheFor := func(opts map[string]interface{}) interface{} {
		if validator == nil {
			return nil
		}
		return validator.GetCacheControlFromOptions(opts)
	}

	lastUserMessageIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleUser {
			lastUserMessageIndex = i
			break
		}
	}

	result := make([]map[string]interface{}, 0, len(messages))
	for index, msg := range messages {
		messageCC := cacheFor(msg.ProviderOptions)

		switch msg.Role {
		case types.RoleSystem:
			text := extractText(msg.Content)
			if messageCC != nil {
				result = append(result, map[string]interface{}{
					"role": "system",
					"content": []map[string]interface{}{
						{"type": "text", "text": text, "cache_control": messageCC},
					},
				})
			} else {
				result = append(result, map[string]interface{}{"role": "system", "content": text})
			}

		case types.RoleUser:
			parts := make([]map[string]interface{}, 0, len(msg.Content))
			for i, part := range msg.Content {
				isLast := i == len(msg.Content)-1
				var partOpts map[string]interface{}
				switch cp := part.(type) {
				case types.TextContent:
					partOpts = cp.ProviderOptions
				case types.ImageContent:
					partOpts = cp.ProviderOptions
				case types.FileContent:
					partOpts = cp.ProviderOptions
				}
				partCC := cacheFor(partOpts)
				if partCC == nil && isLast {
					partCC = messageCC
				}

				var wire map[string]interface{}
				switch cp := part.(type) {
				case types.TextContent:
					wire = map[string]interface{}{"type": "text", "text": cp.Text}
				case types.ImageContent:
					url, err := alibabaImageURL(types.FileContent{MediaType: cp.MimeType, Data: cp.Image, URL: cp.URL})
					if err != nil {
						return nil, err
					}
					wire = map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}}
				case types.FileContent:
					url, err := alibabaImageURL(cp)
					if err != nil {
						return nil, err
					}
					wire = map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}}
				default:
					continue
				}
				if partCC != nil {
					wire["cache_control"] = partCC
				}
				parts = append(parts, wire)
			}
			result = append(result, map[string]interface{}{"role": "user", "content": parts})

		case types.RoleAssistant:
			var text, reasoning strings.Builder
			for _, part := range msg.Content {
				switch cp := part.(type) {
				case types.TextContent:
					text.WriteString(cp.Text)
				case types.ReasoningContent:
					// Reasoning from the current round (after the last user
					// message) always accompanies tool calls; earlier rounds
					// are replayed only when preserved thinking is enabled.
					if preserveThinking || index > lastUserMessageIndex {
						reasoning.WriteString(cp.Text)
					}
				}
			}
			toolCalls := msg.ToolCalls
			if len(toolCalls) == 0 {
				for _, part := range msg.Content {
					if tc, ok := part.(types.ToolCallContent); ok {
						toolCalls = append(toolCalls, types.ToolCall{ID: tc.ToolCallID, ToolName: tc.ToolName, Arguments: tc.Arguments, RawArguments: tc.Input})
					}
				}
			}
			textStr, reasoningStr := text.String(), reasoning.String()
			if textStr == "" && len(toolCalls) == 0 && reasoningStr == "" {
				continue
			}
			entry := map[string]interface{}{"role": "assistant"}
			switch {
			case textStr == "" && len(toolCalls) == 0 && reasoningStr != "":
				entry["content"] = nil
			case messageCC != nil:
				entry["content"] = []map[string]interface{}{{"type": "text", "text": textStr, "cache_control": messageCC}}
			case textStr == "":
				entry["content"] = nil
			default:
				entry["content"] = textStr
			}
			if reasoningStr != "" {
				entry["reasoning_content"] = reasoningStr
			}
			if len(toolCalls) > 0 {
				entry["tool_calls"] = alibabaOpenAIStyleToolCalls(toolCalls)
			}
			result = append(result, entry)

		case types.RoleTool:
			var responses []types.ToolResultContent
			for _, part := range msg.Content {
				if tr, ok := part.(types.ToolResultContent); ok {
					responses = append(responses, tr)
				}
			}
			for i, tr := range responses {
				partCC := cacheFor(tr.ProviderOptions)
				if partCC == nil && i == len(responses)-1 {
					partCC = messageCC
				}
				contentValue := extractToolResultText(tr)
				var content interface{} = contentValue
				if partCC != nil {
					content = []map[string]interface{}{{"type": "text", "text": contentValue, "cache_control": partCC}}
				}
				result = append(result, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tr.ToolCallID,
					"content":      content,
				})
			}
		}
	}
	return result, nil
}

// alibabaImageURL mirrors TS formatImageUrl plus the file-part guards in
// convertToAlibabaChatMessages: only image URL/data file parts are supported.
func alibabaImageURL(fc types.FileContent) (string, error) {
	normalized, err := prompt.NormalizeFileContent(fc)
	if err != nil {
		return "", err
	}
	switch normalized.FileData.Type {
	case types.FileDataTypeReference:
		return "", &providererrors.UnsupportedFunctionalityError{Functionality: "file parts with provider references"}
	case types.FileDataTypeText:
		return "", &providererrors.UnsupportedFunctionalityError{Functionality: "text file parts"}
	}
	mediaType := normalized.MediaType
	topLevel := mediaType
	if idx := strings.IndexByte(mediaType, '/'); idx >= 0 {
		topLevel = mediaType[:idx]
	}
	if topLevel != "image" {
		return "", &providererrors.UnsupportedFunctionalityError{Functionality: "Only image file parts are supported"}
	}
	if normalized.FileData.Type == types.FileDataTypeURL {
		return normalized.FileData.URL, nil
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(normalized.FileData.Data), nil
}
