package moonshot

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// supportedImageMediaTypes are the image media types Moonshot Chat
// Completions accepts as image_url content. Mirrors TS
// supportedImageMediaTypes in convert-to-moonshotai-chat-messages.ts.
var supportedImageMediaTypes = []string{
	"image/jpeg",
	"image/png",
	"image/gif",
	"image/webp",
	"image/bmp",
	"image/heic",
	"image/heif",
}

// supportedVideoMediaTypes are the video media types Moonshot Chat
// Completions accepts as video_url content. Mirrors TS
// supportedVideoMediaTypes.
var supportedVideoMediaTypes = []string{
	"video/mp4",
	"video/mpeg",
	"video/mov",
	"video/avi",
	"video/x-flv",
	"video/mpg",
	"video/webm",
	"video/wmv",
	"video/3gpp",
}

// convertToMoonshotChatMessages converts SDK messages into Moonshot Chat
// Completions wire messages. Mirrors TS convertToMoonshotAIChatMessages.
// responseFormatType is the *wire* response_format.type ("json_schema" |
// "json_object" | ""), used to validate Partial Mode.
func convertToMoonshotChatMessages(modelID string, messages []types.Message, responseFormatType string) ([]interface{}, []types.Warning, error) {
	out := make([]interface{}, 0, len(messages))
	var warnings []types.Warning
	modelFamily := GetModelFamily(modelID)

	for index, msg := range messages {
		msgOpts, err := parseMoonshotMessageOptions(msg.ProviderOptions)
		if err != nil {
			return nil, nil, err
		}

		if msgOpts != nil && msgOpts.Partial && msg.Role != types.RoleAssistant {
			return nil, nil, &providererrors.InvalidArgumentError{
				Field:   "prompt",
				Message: "Moonshot AI Partial Mode requires `partial: true` on an assistant message.",
			}
		}
		if msgOpts != nil && len(msgOpts.Tools) > 0 && msg.Role != types.RoleSystem {
			return nil, nil, &providererrors.InvalidArgumentError{
				Field:   "prompt",
				Message: "Moonshot dynamic tools must be configured on a system message.",
			}
		}

		switch msg.Role {
		case types.RoleSystem:
			if msgOpts != nil && len(msgOpts.Tools) > 0 {
				if extractMoonshotText(msg.Content) != "" {
					return nil, nil, &providererrors.InvalidArgumentError{
						Field:   "prompt",
						Message: "A Moonshot dynamic-tool system message must use empty content because the API forbids content alongside tools.",
					}
				}
				if modelFamily != FamilyKimiK3 && modelFamily != FamilyUnknown {
					details := "Moonshot documents dynamic tool loading only for Kimi K3. The dynamic system message has been omitted."
					warnings = append(warnings, types.Warning{
						Type:    "unsupported",
						Feature: fmt.Sprintf("dynamic tool loading for model %q", modelID),
						Details: details,
						Message: details,
					})
					continue
				}
				moonshotTools, _, toolWarnings, err := prepareMoonshotTools(msgOpts.Tools, types.ToolChoice{}, false, modelID)
				if err != nil {
					return nil, nil, err
				}
				warnings = append(warnings, toolWarnings...)
				if moonshotTools == nil {
					moonshotTools = []moonshotFunctionTool{}
				}
				out = append(out, map[string]interface{}{"role": "system", "tools": moonshotTools})
				continue
			}

			systemMsg := map[string]interface{}{"role": "system", "content": extractMoonshotText(msg.Content)}
			if msgOpts != nil && msgOpts.HasName {
				systemMsg["name"] = msgOpts.Name
			}
			out = append(out, systemMsg)

		case types.RoleUser:
			if len(msg.Content) == 1 {
				if tc, ok := msg.Content[0].(types.TextContent); ok {
					userMsg := map[string]interface{}{"role": "user", "content": tc.Text}
					if msgOpts != nil && msgOpts.HasName {
						userMsg["name"] = msgOpts.Name
					}
					out = append(out, userMsg)
					break
				}
			}

			parts := make([]interface{}, 0, len(msg.Content))
			for _, part := range msg.Content {
				converted, err := convertMoonshotUserContentPart(part)
				if err != nil {
					return nil, nil, err
				}
				if converted != nil {
					parts = append(parts, converted)
				}
			}
			userMsg := map[string]interface{}{"role": "user", "content": parts}
			if msgOpts != nil && msgOpts.HasName {
				userMsg["name"] = msgOpts.Name
			}
			out = append(out, userMsg)

		case types.RoleAssistant:
			if msgOpts != nil && msgOpts.Partial {
				if index != len(messages)-1 {
					return nil, nil, &providererrors.InvalidArgumentError{
						Field:   "prompt",
						Message: "Moonshot AI Partial Mode requires the partial assistant message to be the final message.",
					}
				}
				if responseFormatType == "json_object" {
					return nil, nil, &providererrors.InvalidArgumentError{
						Field:   "prompt",
						Message: "Moonshot AI Partial Mode cannot be combined with JSON object response format.",
					}
				}
			}

			var text, reasoning strings.Builder
			for _, part := range msg.Content {
				switch cp := part.(type) {
				case types.TextContent:
					text.WriteString(cp.Text)
				case types.ReasoningContent:
					reasoning.WriteString(cp.Text)
				}
			}

			hasToolCalls := len(msg.ToolCalls) > 0
			assistantMsg := map[string]interface{}{"role": "assistant"}
			if hasToolCalls {
				if text.Len() > 0 {
					assistantMsg["content"] = text.String()
				} else {
					assistantMsg["content"] = nil
				}
			} else {
				assistantMsg["content"] = text.String()
			}
			if msgOpts != nil && msgOpts.HasName {
				assistantMsg["name"] = msgOpts.Name
			}
			if msgOpts != nil && msgOpts.Partial {
				assistantMsg["partial"] = true
			}
			if reasoning.Len() > 0 {
				assistantMsg["reasoning_content"] = reasoning.String()
			}
			if hasToolCalls {
				assistantMsg["tool_calls"] = moonshotWireToolCalls(msg.ToolCalls)
			}
			out = append(out, assistantMsg)

		case types.RoleTool:
			if msgOpts != nil && msgOpts.HasName {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "message name on tool messages",
				})
			}
			for _, part := range msg.Content {
				tr, ok := part.(types.ToolResultContent)
				if !ok {
					continue
				}
				out = append(out, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tr.ToolCallID,
					"content":      extractMoonshotToolResultText(tr),
				})
			}
		}
	}

	return out, warnings, nil
}

func extractMoonshotText(parts []types.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if tc, ok := part.(types.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func moonshotWireToolCalls(toolCalls []types.ToolCall) []map[string]interface{} {
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

func extractMoonshotToolResultText(tr types.ToolResultContent) string {
	if tr.Output != nil {
		switch tr.Output.Type {
		case types.ToolResultOutputText, types.ToolResultOutputErrorText:
			if v, ok := tr.Output.Value.(string); ok {
				return v
			}
		case types.ToolResultOutputExecutionDenied:
			if tr.Output.Reason != "" {
				return tr.Output.Reason
			}
			return "Tool call execution denied."
		case types.ToolResultOutputJSON, types.ToolResultOutputContent, types.ToolResultOutputErrorJSON:
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
	return tr.Error
}

// topLevelMoonshotMediaType returns the portion of a media type before "/",
// mirroring TS getTopLevelMediaType.
func topLevelMoonshotMediaType(mediaType string) string {
	if idx := strings.IndexByte(mediaType, '/'); idx >= 0 {
		return mediaType[:idx]
	}
	return mediaType
}

func containsMoonshotMediaType(list []string, mediaType string) bool {
	for _, v := range list {
		if v == mediaType {
			return true
		}
	}
	return false
}

// convertMoonshotUserContentPart converts a single user-message content part
// to Moonshot's wire content-part shape (text / image_url / video_url).
// Mirrors the per-part switch in TS convertToMoonshotAIChatMessages.
func convertMoonshotUserContentPart(part types.ContentPart) (map[string]interface{}, error) {
	switch cp := part.(type) {
	case types.TextContent:
		return map[string]interface{}{"type": "text", "text": cp.Text}, nil
	case types.ImageContent:
		fc := types.FileContent{Data: cp.Image, MediaType: cp.MimeType, URL: cp.URL}
		return convertMoonshotFilePart(fc)
	case types.FileContent:
		return convertMoonshotFilePart(cp)
	default:
		return nil, nil
	}
}

func convertMoonshotFilePart(fc types.FileContent) (map[string]interface{}, error) {
	normalized, err := prompt.NormalizeFileContent(fc)
	if err != nil {
		return nil, err
	}
	topLevel := topLevelMoonshotMediaType(normalized.MediaType)

	switch normalized.FileData.Type {
	case types.FileDataTypeReference:
		if topLevel != "image" && topLevel != "video" {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "file part media type " + normalized.MediaType,
			}
		}
		ref, err := providerutils.ResolveProviderReference(normalized.FileData.Reference, "moonshot")
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(ref, "ms://") {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "Moonshot file provider references without an ms:// URL",
			}
		}
		if topLevel == "image" {
			return map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": ref}}, nil
		}
		return map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": ref}}, nil

	case types.FileDataTypeText:
		if topLevel == "text" {
			return map[string]interface{}{"type": "text", "text": normalized.FileData.Text}, nil
		}
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "file part media type " + normalized.MediaType,
		}

	case types.FileDataTypeURL, types.FileDataTypeData:
		switch topLevel {
		case "image":
			url, err := formatMoonshotMediaURL(normalized.FileData, supportedImageMediaTypes, "image")
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}}, nil
		case "video":
			url, err := formatMoonshotMediaURL(normalized.FileData, supportedVideoMediaTypes, "video")
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": url}}, nil
		case "text":
			var textContent string
			if normalized.FileData.Type == types.FileDataTypeURL {
				textContent = normalized.FileData.URL
			} else {
				textContent = string(normalized.FileData.Data)
			}
			return map[string]interface{}{"type": "text", "text": textContent}, nil
		default:
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "file part media type " + normalized.MediaType,
			}
		}
	}

	return nil, &providererrors.UnsupportedFunctionalityError{
		Functionality: "file part media type " + normalized.MediaType,
	}
}

// formatMoonshotMediaURL mirrors TS formatMediaUrl: a Data-type part must use
// one of the explicitly supported media types; a URL-type part is also
// allowed when its media type is the bare top-level type or "<top>/*"
// (e.g. an unresolved "image/*" wildcard from supportedUrls negotiation).
func formatMoonshotMediaURL(fd types.FileData, supportedMediaTypes []string, topLevelMediaType string) (string, error) {
	mediaType := fd.MediaType
	allowed := containsMoonshotMediaType(supportedMediaTypes, mediaType)
	if !allowed && fd.Type == types.FileDataTypeURL && (mediaType == topLevelMediaType || mediaType == topLevelMediaType+"/*") {
		allowed = true
	}
	if !allowed {
		return "", &providererrors.UnsupportedFunctionalityError{
			Functionality: "file part media type " + mediaType,
		}
	}
	if fd.Type == types.FileDataTypeURL {
		return fd.URL, nil
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(fd.Data), nil
}
