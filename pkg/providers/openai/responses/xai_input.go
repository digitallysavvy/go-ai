package responses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// ConvertPromptToXAIInput converts a prompt to the xAI Responses API input
// array. It ports TS convertToXaiResponsesInput
// (packages/xai/src/responses/convert-to-xai-responses-input.ts), which is a
// separate, simpler converter than the OpenAI one: system messages always use
// the "system" role, assistant text and function calls carry the xAI item
// ids, reasoning parts are replayed as reasoning items, tool errors are sent
// verbatim, and non-image inline files are rejected.
func ConvertPromptToXAIInput(prompt types.Prompt) ([]interface{}, []types.Warning, error) {
	input := make([]interface{}, 0, len(prompt.Messages)+1)
	var warnings []types.Warning

	if prompt.System != "" {
		input = append(input, SystemMessage{Role: "system", Content: prompt.System})
	}

	for _, msg := range prompt.Messages {
		switch msg.Role {
		case types.RoleSystem:
			input = append(input, SystemMessage{Role: "system", Content: systemMessageText(msg)})
		case types.RoleUser:
			parts, err := convertXAIUserParts(msg)
			if err != nil {
				return nil, warnings, err
			}
			input = append(input, UserMessage{Role: "user", Content: parts})
		case types.RoleAssistant:
			items, w := convertXAIAssistantItems(msg)
			input = append(input, items...)
			warnings = append(warnings, w...)
		case types.RoleTool:
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
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": tr.ToolCallID,
					"output":  xaiToolResultOutput(tr),
				})
			}
		}
	}
	return input, warnings, nil
}

// xaiOption reads a string option the way TS does for xAI parts: the part's
// providerOptions.xai (output-direction providerMetadata.xai is accepted too,
// since Go callers replay model output parts as-is).
func xaiPartString(options map[string]interface{}, metadata json.RawMessage, key string) (string, bool) {
	if opts, ok := options["xai"].(map[string]interface{}); ok {
		if v, ok := opts[key].(string); ok {
			return v, true
		}
	}
	if len(metadata) > 0 {
		var raw map[string]interface{}
		if json.Unmarshal(metadata, &raw) == nil {
			if opts, ok := raw["xai"].(map[string]interface{}); ok {
				if v, ok := opts[key].(string); ok {
					return v, true
				}
			}
		}
	}
	return "", false
}

// xaiFilePartOptionsSchema mirrors TS xaiFilePartProviderOptions.
var xaiFilePartOptionsSchema = providerutils.OptionSchema{"imageDetail": providerutils.OptEnum("low", "high", "auto")}

// xaiImageDetail parses providerOptions.xai on an image part the way TS
// parseProviderOptions does: an invalid imageDetail fails the call.
func xaiImageDetail(options map[string]interface{}) (string, error) {
	parsed, err := providerutils.ParseProviderOptions("xai", options, xaiFilePartOptionsSchema)
	if err != nil {
		return "", err
	}
	detail, _ := parsed["imageDetail"].(string)
	return detail, nil
}

func convertXAIUserParts(msg types.Message) ([]interface{}, error) {
	parts := make([]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			parts = append(parts, UserTextPart{Type: "input_text", Text: p.Text})
		case types.ImageContent:
			imageURL := p.URL
			if imageURL == "" && len(p.Image) > 0 {
				imageURL = fmt.Sprintf("data:%s;base64,%s", p.MimeType, base64.StdEncoding.EncodeToString(p.Image))
			}
			if imageURL == "" {
				continue
			}
			detail, err := xaiImageDetail(p.ProviderOptions)
			if err != nil {
				return nil, err
			}
			parts = append(parts, UserImageURLPart{Type: "input_image", ImageURL: imageURL, Detail: detail})
		case types.FileContent:
			item, err := convertXAIFilePart(p)
			if err != nil {
				return nil, err
			}
			if item != nil {
				parts = append(parts, item)
			}
		}
	}
	return parts, nil
}

func convertXAIFilePart(p types.FileContent) (interface{}, error) {
	mediaType := openAIFileMediaType(p)
	fileDataType := p.FileData.Type
	if fileDataType == "" {
		switch {
		case p.URL != "":
			fileDataType = types.FileDataTypeURL
		case p.Reference != "":
			fileDataType = types.FileDataTypeReference
		case p.Text != "":
			fileDataType = types.FileDataTypeText
		case len(p.Data) > 0:
			fileDataType = types.FileDataTypeData
		}
	}
	switch fileDataType {
	case types.FileDataTypeReference:
		reference, err := resolveProviderReferenceStrict(p.FileData.Reference, "xai")
		if err != nil {
			return nil, err
		}
		reference = firstNonEmpty(reference, p.Reference)
		if reference == "" {
			return nil, nil
		}
		return map[string]interface{}{"type": "input_file", "file_id": reference}, nil
	case types.FileDataTypeText:
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "text file parts"}
	case types.FileDataTypeURL, types.FileDataTypeData:
		fileURL := firstNonEmpty(p.FileData.URL, p.URL)
		isImage := isImageMediaType(mediaType)
		if isImage {
			imageURL := fileURL
			if fileDataType == types.FileDataTypeData {
				data := openAIFileDataBase64(p)
				if data == "" {
					return nil, nil
				}
				resolvedMediaType, resolveErr := resolveInlineBase64MediaType(mediaType, data)
				if resolveErr != nil {
					return nil, resolveErr
				}
				imageURL = fmt.Sprintf("data:%s;base64,%s", resolvedMediaType, data)
			}
			if imageURL == "" {
				return nil, nil
			}
			detail, err := xaiImageDetail(p.ProviderOptions)
			if err != nil {
				return nil, err
			}
			return UserImageURLPart{Type: "input_image", ImageURL: imageURL, Detail: detail}, nil
		}
		if fileDataType == types.FileDataTypeURL {
			if fileURL == "" {
				return nil, nil
			}
			// xAI accepts non-image documents (PDF, text, CSV, ...) by URL.
			return UserFilePart{Type: "input_file", FileURL: fileURL}, nil
		}
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("file part media type %s as inline data (xAI Responses requires a URL or a Files API reference for non-image files)", mediaType),
		}
	}
	return nil, nil
}

func convertXAIAssistantItems(msg types.Message) ([]interface{}, []types.Warning) {
	var items []interface{}
	var warnings []types.Warning
	seenCalls := map[string]bool{}

	toolCall := func(id, callID, name, arguments string) {
		items = append(items, FunctionCallItem{
			Type:      "function_call",
			ID:        firstNonEmpty(id, callID),
			CallID:    callID,
			Name:      name,
			Arguments: arguments,
			Status:    "completed",
		})
	}

	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			item := map[string]interface{}{"role": "assistant", "content": p.Text}
			if id, ok := xaiPartString(p.ProviderOptions, p.ProviderMetadata, "itemId"); ok {
				item["id"] = id
			}
			items = append(items, item)
		case types.ToolCallContent:
			seenCalls[p.ToolCallID] = true
			if p.ProviderExecuted {
				continue
			}
			id, _ := xaiPartString(p.ProviderOptions, p.ProviderMetadata, "itemId")
			_, args := toolCallContentArguments(p)
			toolCall(id, p.ToolCallID, p.ToolName, args)
		case *types.ToolCallContent:
			if p == nil {
				continue
			}
			seenCalls[p.ToolCallID] = true
			if p.ProviderExecuted {
				continue
			}
			id, _ := xaiPartString(p.ProviderOptions, p.ProviderMetadata, "itemId")
			_, args := toolCallContentArguments(*p)
			toolCall(id, p.ToolCallID, p.ToolName, args)
		case types.ToolResultContent, *types.ToolResultContent:
			// Provider-executed results are not replayed.
		case types.ReasoningContent:
			itemID, hasID := xaiPartString(p.ProviderOptions, p.ProviderMetadata, "itemId")
			encrypted, hasEncrypted := xaiPartString(p.ProviderOptions, p.ProviderMetadata, "reasoningEncryptedContent")
			if !hasEncrypted && p.EncryptedContent != "" {
				encrypted, hasEncrypted = p.EncryptedContent, true
			}
			if hasID || hasEncrypted {
				summary := []map[string]interface{}{}
				if len(p.Text) > 0 {
					summary = append(summary, map[string]interface{}{"type": "summary_text", "text": p.Text})
				}
				item := map[string]interface{}{
					"type":    "reasoning",
					"id":      itemID,
					"summary": summary,
					"status":  "completed",
				}
				if hasEncrypted {
					item["encrypted_content"] = encrypted
				}
				items = append(items, item)
			} else {
				warnings = append(warnings, types.Warning{
					Type:    "other",
					Message: "Reasoning parts without itemId or encrypted content cannot be sent back to xAI. Skipping.",
				})
			}
		case types.ReasoningFileContent, *types.ReasoningFileContent:
			warnings = append(warnings, xaiUnsupportedAssistantPart("reasoning-file"))
		case types.CustomContent, *types.CustomContent:
			warnings = append(warnings, xaiUnsupportedAssistantPart("custom"))
		case types.FileContent, *types.FileContent:
			warnings = append(warnings, xaiUnsupportedAssistantPart("file"))
		}
	}

	// Legacy Message.ToolCalls carry the same client-side function calls.
	for _, tc := range msg.ToolCalls {
		if seenCalls[tc.ID] || tc.ProviderExecuted {
			continue
		}
		id := ""
		if meta, ok := tc.ProviderMetadata["xai"].(map[string]interface{}); ok {
			id, _ = meta["itemId"].(string)
		}
		args := tc.RawArguments
		if args == "" {
			b, _ := jsonstr.Marshal(tc.Arguments)
			args = string(b)
		}
		toolCall(id, tc.ID, tc.ToolName, args)
	}
	return items, warnings
}

func xaiUnsupportedAssistantPart(kind string) types.Warning {
	return types.Warning{
		Type:    "other",
		Message: fmt.Sprintf("xAI Responses API does not support %s in assistant messages", kind),
	}
}

// xaiToolResultOutput maps a tool result to function_call_output.output:
// text and error text verbatim, JSON values stringified, and content blocks
// as input_text / input_image parts (other blocks are dropped).
func xaiToolResultOutput(tr types.ToolResultContent) interface{} {
	out := tr.Output
	if out == nil {
		if tr.Error != "" {
			return tr.Error
		}
		if s, ok := tr.Result.(string); ok {
			return s
		}
		b, err := jsonstr.Marshal(tr.Result)
		if err != nil {
			return ""
		}
		return string(b)
	}
	switch out.Type {
	case types.ToolResultOutputText, types.ToolResultOutputErrorText, types.ToolResultOutputError:
		if s, ok := out.Value.(string); ok {
			return s
		}
		b, _ := jsonstr.Marshal(out.Value)
		return string(b)
	case types.ToolResultOutputExecutionDenied:
		if out.Reason != "" {
			return out.Reason
		}
		return "tool execution denied"
	case types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON:
		b, _ := jsonstr.Marshal(out.Value)
		return string(b)
	case types.ToolResultOutputContent:
		parts := []interface{}{}
		for _, block := range out.Content {
			switch b := block.(type) {
			case types.TextContentBlock:
				parts = append(parts, map[string]interface{}{"type": "input_text", "text": b.Text})
			case types.ImageContentBlock:
				parts = append(parts, map[string]interface{}{
					"type":      "input_image",
					"image_url": fmt.Sprintf("data:%s;base64,%s", b.MediaType, base64.StdEncoding.EncodeToString(b.Data)),
				})
			case types.FileContentBlock:
				mediaType := firstNonEmpty(b.MediaType, b.FileData.MediaType)
				if !isImageMediaType(mediaType) {
					continue
				}
				switch {
				case b.FileData.Type == types.FileDataTypeURL || (b.FileData.Type == "" && b.URL != ""):
					if u := firstNonEmpty(b.FileData.URL, b.URL); u != "" {
						parts = append(parts, map[string]interface{}{"type": "input_image", "image_url": u})
					}
				default:
					if data := fileContentBlockDataBase64(b); data != "" {
						// TS resolveFullMediaType sniffs a bare or wildcard type.
						if resolved, err := resolveInlineBase64MediaType(mediaType, data); err == nil {
							mediaType = resolved
						}
						parts = append(parts, map[string]interface{}{
							"type":      "input_image",
							"image_url": fmt.Sprintf("data:%s;base64,%s", mediaType, data),
						})
					}
				}
			}
		}
		return parts
	}
	return strings.TrimSpace(fmt.Sprint(out.Value))
}
