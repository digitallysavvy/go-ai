package deepseek

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// deepseekMaxImageURLLength mirrors the TypeScript SDK's 8192 character limit
// on DeepSeek image_url values.
const deepseekMaxImageURLLength = 8192

var deepseekSupportedImageMediaTypes = map[string]bool{
	"image/gif":  true,
	"image/jpeg": true,
	"image/jpg":  true,
	"image/png":  true,
	"image/webp": true,
}

func firstNonEmptyDS(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// deepseekProviderOptionsMap extracts the provider-options block for the
// given provider namespace (usually "deepseek") from a generic
// providerOptions map keyed by provider name.
func deepseekProviderOptionsMap(providerOptionsName string, providerOptions map[string]interface{}) map[string]interface{} {
	if providerOptions == nil {
		return nil
	}
	if m, ok := providerOptions[providerOptionsName].(map[string]interface{}); ok {
		return m
	}
	return nil
}

// convertMessages converts unified messages to DeepSeek's chat completions
// wire format. It mirrors the TypeScript SDK's convertToDeepSeekChatMessages:
// image content parts (inline data, URLs, and provider file references),
// per-message `name`/`prefix` provider options, and the V4 reasoning_content
// back-fill behavior.
func (m *LanguageModel) convertMessages(messages []types.Message) ([]map[string]interface{}, []types.Warning, error) {
	providerOptionsName := m.provider.providerOptionsName()
	isV4 := isDeepSeekV4Model(m.modelID)
	supportsPrefix := m.provider.supportsBeta()

	var warnings []types.Warning
	result := make([]map[string]interface{}, 0, len(messages))

	lastUserMessageIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleUser {
			lastUserMessageIndex = i
			break
		}
	}

	for index, msg := range messages {
		msgOpts := deepseekProviderOptionsMap(providerOptionsName, msg.ProviderOptions)
		name, hasName := msgOpts["name"].(string)
		prefix, _ := msgOpts["prefix"].(bool)

		if prefix && msg.Role != types.RoleAssistant {
			return nil, nil, &providererrors.InvalidArgumentError{
				Field:   "prompt",
				Message: "DeepSeek assistant prefix completion requires `prefix: true` on an assistant message.",
			}
		}

		switch msg.Role {
		case types.RoleSystem:
			entry := map[string]interface{}{"role": "system", "content": deepseekTextOnly(msg.Content)}
			if hasName && name != "" {
				entry["name"] = name
			}
			result = append(result, entry)

		case types.RoleUser:
			entry, userWarnings, err := m.convertUserMessage(msg, providerOptionsName)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, userWarnings...)
			if hasName && name != "" {
				entry["name"] = name
			}
			result = append(result, entry)

		case types.RoleAssistant:
			if prefix {
				if index != len(messages)-1 {
					return nil, nil, &providererrors.InvalidArgumentError{
						Field:   "prompt",
						Message: "DeepSeek assistant prefix completion requires the prefixed assistant message to be the final message.",
					}
				}
				if !supportsPrefix {
					return nil, nil, &providererrors.UnsupportedFunctionalityError{
						Functionality: "DeepSeek assistant prefix completion",
						Message:       "DeepSeek assistant prefix completion requires a beta base URL ending in `/beta`.",
					}
				}
			}
			entry := deepseekConvertAssistantMessage(msg, index, lastUserMessageIndex, isV4)
			if hasName && name != "" {
				entry["name"] = name
			}
			if prefix {
				entry["prefix"] = true
			}
			result = append(result, entry)

		case types.RoleTool:
			if hasName && name != "" {
				warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "message name on tool messages"})
			}
			entries, toolWarnings, err := m.convertToolMessage(msg, providerOptionsName)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, toolWarnings...)
			result = append(result, entries...)

		default:
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("message role: %s", msg.Role)})
		}
	}

	return result, warnings, nil
}

func deepseekTextOnly(parts []types.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if t, ok := part.(types.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func deepseekIsImagePart(part types.ContentPart) bool {
	switch p := part.(type) {
	case types.ImageContent:
		return true
	case types.FileContent:
		// Inline text document data (FileDataTypeText) is never image data,
		// even if a caller mistakenly also set an image/* media type —
		// mirrors the TS SDK's hasImagePart check, which only treats
		// reference/url/data file-data shapes as images.
		if p.FileData.Type == types.FileDataTypeText {
			return false
		}
		return deepseekIsImageMediaType(firstNonEmptyDS(p.MediaType, p.MimeType, p.FileData.MediaType))
	}
	return false
}

func deepseekIsImageMediaType(mediaType string) bool {
	mediaType = strings.ToLower(mediaType)
	return mediaType == "image" || strings.HasPrefix(mediaType, "image/")
}

func (m *LanguageModel) convertUserMessage(msg types.Message, providerOptionsName string) (map[string]interface{}, []types.Warning, error) {
	hasImage := false
	for _, part := range msg.Content {
		if deepseekIsImagePart(part) {
			hasImage = true
			break
		}
	}

	var warnings []types.Warning

	if !hasImage {
		var text strings.Builder
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.TextContent:
				text.WriteString(p.Text)
			default:
				warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("user message part type: %s", part.ContentType())})
			}
		}
		return map[string]interface{}{"role": "user", "content": text.String()}, warnings, nil
	}

	contentParts := make([]map[string]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			contentParts = append(contentParts, map[string]interface{}{"type": "text", "text": p.Text})
		case types.ImageContent:
			cp, err := deepseekResolveUserImagePart(imageContentAsFileContent(p), providerOptionsName)
			if err != nil {
				return nil, nil, err
			}
			contentParts = append(contentParts, cp)
		case types.FileContent:
			if !deepseekIsImagePart(p) {
				warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "user message part type: file"})
				continue
			}
			cp, err := deepseekResolveUserImagePart(p, providerOptionsName)
			if err != nil {
				return nil, nil, err
			}
			contentParts = append(contentParts, cp)
		default:
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("user message part type: %s", part.ContentType())})
		}
	}

	return map[string]interface{}{"role": "user", "content": contentParts}, warnings, nil
}

func imageContentAsFileContent(p types.ImageContent) types.FileContent {
	return types.FileContent{
		MediaType:       p.MimeType,
		Data:            p.Image,
		URL:             p.URL,
		ProviderOptions: p.ProviderOptions,
	}
}

// deepseekResolveImageMediaType validates that a file/image content part's
// media type is one of DeepSeek's supported image formats.
func deepseekResolveImageMediaType(mediaType string) (string, error) {
	normalized := strings.ToLower(mediaType)
	if !deepseekSupportedImageMediaTypes[normalized] {
		return "", &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("DeepSeek image media type %s", normalized),
			Message:       "DeepSeek supports JPEG, PNG, GIF, and WebP image inputs.",
		}
	}
	return normalized, nil
}

func deepseekImageDataURL(mediaType string, data []byte) string {
	wireMediaType := mediaType
	if wireMediaType == "image/jpg" {
		wireMediaType = "image/jpeg"
	}
	return fmt.Sprintf("data:%s;base64,%s", wireMediaType, base64.StdEncoding.EncodeToString(data))
}

// deepseekResolveUserImagePart builds a user-message content part for an
// image file (reference -> `file`/file_id, URL/data -> `image_url`, or a
// `file`/file_data part when the `fileData` provider option requests inline
// file semantics instead of image_url). Mirrors the TypeScript SDK's inline
// handling in convertToDeepSeekChatMessages for user messages.
func deepseekResolveUserImagePart(f types.FileContent, providerOptionsName string) (map[string]interface{}, error) {
	fileOpts := deepseekProviderOptionsMap(providerOptionsName, f.ProviderOptions)
	imageDetail, _ := fileOpts["imageDetail"].(string)
	fileData, _ := fileOpts["fileData"].(bool)

	if ref := deepseekFileReference(f); ref != "" {
		return map[string]interface{}{"type": "file", "file_id": ref}, nil
	}

	mediaType, err := deepseekResolveImageMediaType(firstNonEmptyDS(f.MediaType, f.MimeType, f.FileData.MediaType))
	if err != nil {
		return nil, err
	}

	if url := deepseekFileURL(f); url != "" {
		if len(url) > deepseekMaxImageURLLength {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "prompt",
				Message: "DeepSeek image URLs must not exceed 8192 characters.",
			}
		}
		if fileData {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "prompt",
				Message: "DeepSeek `fileData` image parts require inline data, not a URL.",
			}
		}
		imageURL := map[string]interface{}{"url": url}
		if imageDetail != "" {
			imageURL["detail"] = imageDetail
		}
		return map[string]interface{}{"type": "image_url", "image_url": imageURL}, nil
	}

	data := deepseekFileBytes(f)
	dataURL := deepseekImageDataURL(mediaType, data)

	if fileData {
		if imageDetail != "" {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "prompt",
				Message: "DeepSeek `imageDetail` cannot be combined with `fileData`.",
			}
		}
		part := map[string]interface{}{"type": "file", "file_data": dataURL}
		if f.Filename != "" {
			part["filename"] = f.Filename
		}
		return part, nil
	}

	imageURL := map[string]interface{}{"url": dataURL}
	if imageDetail != "" {
		imageURL["detail"] = imageDetail
	}
	return map[string]interface{}{"type": "image_url", "image_url": imageURL}, nil
}

// deepseekResolveToolResultImagePart builds a tool-result content part for an
// image block. Unlike user-message image parts, DeepSeek tool results never
// support the `fileData`/`file_data` inline-file shape — only `file`/file_id
// references and `image_url` parts.
func deepseekResolveToolResultImagePart(f types.FileContent, providerOptionsName string) (map[string]interface{}, error) {
	if ref := deepseekFileReference(f); ref != "" {
		return map[string]interface{}{"type": "file", "file_id": ref}, nil
	}

	mediaType, err := deepseekResolveImageMediaType(firstNonEmptyDS(f.MediaType, f.MimeType, f.FileData.MediaType))
	if err != nil {
		return nil, err
	}

	fileOpts := deepseekProviderOptionsMap(providerOptionsName, f.ProviderOptions)
	imageDetail, _ := fileOpts["imageDetail"].(string)

	var url string
	if u := deepseekFileURL(f); u != "" {
		if len(u) > deepseekMaxImageURLLength {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "prompt",
				Message: "DeepSeek image URLs must not exceed 8192 characters.",
			}
		}
		url = u
	} else {
		url = deepseekImageDataURL(mediaType, deepseekFileBytes(f))
	}

	imageURL := map[string]interface{}{"url": url}
	if imageDetail != "" {
		imageURL["detail"] = imageDetail
	}
	return map[string]interface{}{"type": "image_url", "image_url": imageURL}, nil
}

func deepseekFileReference(f types.FileContent) string {
	if f.FileData.Type == types.FileDataTypeReference {
		if ref := types.ProviderReferenceString(f.FileData.Reference); ref != "" {
			return ref
		}
	}
	return f.Reference
}

func deepseekFileURL(f types.FileContent) string {
	if f.FileData.Type == types.FileDataTypeURL && f.FileData.URL != "" {
		return f.FileData.URL
	}
	return f.URL
}

func deepseekFileBytes(f types.FileContent) []byte {
	if f.FileData.Type == types.FileDataTypeData {
		if len(f.FileData.Data) > 0 {
			return f.FileData.Data
		}
		if f.FileData.DataString != "" {
			if decoded, err := types.DecodeFileDataString(f.FileData.DataString); err == nil {
				return decoded
			}
		}
	}
	return f.Data
}

func deepseekConvertAssistantMessage(msg types.Message, index, lastUserMessageIndex int, isV4 bool) map[string]interface{} {
	var text strings.Builder
	var reasoning strings.Builder
	hasReasoning := false

	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			text.WriteString(p.Text)
		case types.ReasoningContent:
			// R1 (and other non-V4 models) must not receive prior-round
			// reasoning; V4 requires it on every turn.
			if index <= lastUserMessageIndex && !isV4 {
				continue
			}
			reasoning.WriteString(p.Text)
			hasReasoning = true
		}
	}

	entry := map[string]interface{}{"role": "assistant", "content": text.String()}
	if len(msg.ToolCalls) > 0 {
		entry["tool_calls"] = deepseekToolCallsWire(msg.ToolCalls)
	}

	switch {
	case hasReasoning:
		entry["reasoning_content"] = reasoning.String()
	case isV4:
		// V4 requires the field on every assistant turn — back-fill an empty
		// string when the source message had no reasoning part at all.
		entry["reasoning_content"] = ""
	}

	return entry
}

func deepseekToolCallsWire(calls []types.ToolCall) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(calls))
	for _, tc := range calls {
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

func (m *LanguageModel) convertToolMessage(msg types.Message, providerOptionsName string) ([]map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	var entries []map[string]interface{}

	for _, part := range msg.Content {
		tr, ok := part.(types.ToolResultContent)
		if !ok {
			continue
		}
		if tr.Output == nil {
			entries = append(entries, deepseekToolMessage(tr.ToolCallID, deepseekStringifyResult(tr.Result)))
			continue
		}

		switch tr.Output.Type {
		case types.ToolResultOutputText, types.ToolResultOutputErrorText:
			entries = append(entries, deepseekToolMessage(tr.ToolCallID, deepseekStringValue(tr.Output.Value)))

		case types.ToolResultOutputExecutionDenied:
			reason := tr.Output.Reason
			if reason == "" {
				reason = "Tool call execution denied."
			}
			entries = append(entries, deepseekToolMessage(tr.ToolCallID, reason))

		case types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON, types.ToolResultOutputError:
			b, _ := json.Marshal(tr.Output.Value)
			entries = append(entries, deepseekToolMessage(tr.ToolCallID, string(b)))

		case types.ToolResultOutputContent:
			hasImage := false
			for _, block := range tr.Output.Content {
				if deepseekIsImageBlock(block) {
					hasImage = true
					break
				}
			}
			if !hasImage {
				b, _ := json.Marshal(tr.Output.Content)
				entries = append(entries, deepseekToolMessage(tr.ToolCallID, string(b)))
				continue
			}

			var contentParts []map[string]interface{}
			for _, block := range tr.Output.Content {
				switch b := block.(type) {
				case types.TextContentBlock:
					contentParts = append(contentParts, map[string]interface{}{"type": "text", "text": b.Text})
				case types.FileContentBlock:
					if !deepseekIsImageBlock(b) {
						warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("tool result content part type: %s", b.ToolResultContentType())})
						continue
					}
					fc := types.FileContent{FileData: b.FileData, Data: b.Data, MediaType: b.MediaType, URL: b.URL, Reference: b.Reference, ProviderOptions: b.ProviderOptions}
					cp, err := deepseekResolveToolResultImagePart(fc, providerOptionsName)
					if err != nil {
						return nil, nil, err
					}
					contentParts = append(contentParts, cp)
				case types.ImageContentBlock:
					fc := types.FileContent{Data: b.Data, MediaType: b.MediaType, ProviderOptions: b.ProviderOptions}
					cp, err := deepseekResolveToolResultImagePart(fc, providerOptionsName)
					if err != nil {
						return nil, nil, err
					}
					contentParts = append(contentParts, cp)
				default:
					warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("tool result content part type: %s", block.ToolResultContentType())})
				}
			}
			entries = append(entries, map[string]interface{}{"role": "tool", "tool_call_id": tr.ToolCallID, "content": contentParts})
		}
	}

	return entries, warnings, nil
}

func deepseekIsImageBlock(block types.ToolResultContentBlock) bool {
	switch b := block.(type) {
	case types.ImageContentBlock:
		return true
	case types.FileContentBlock:
		if b.FileData.Type == types.FileDataTypeText {
			return false
		}
		return deepseekIsImageMediaType(firstNonEmptyDS(b.MediaType, b.FileData.MediaType))
	}
	return false
}

func deepseekToolMessage(toolCallID, content string) map[string]interface{} {
	return map[string]interface{}{"role": "tool", "tool_call_id": toolCallID, "content": content}
}

func deepseekStringValue(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func deepseekStringifyResult(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
