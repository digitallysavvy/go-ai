package deepseek

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// deepseekResponsesImageMediaTypes are the image types the Responses API
// accepts (TS supportedImageMediaTypes).
var deepseekResponsesImageMediaTypes = map[string]bool{
	"image/gif": true, "image/jpeg": true, "image/jpg": true, "image/png": true, "image/webp": true,
}

// convertToDeepSeekResponsesInput is TS convertToDeepSeekResponsesInput
// (convert-to-deepseek-responses-input.ts): system messages are
// {role: system, content}; user text / image parts become input_text /
// input_image; assistant text, reasoning (with its item id), function calls
// and the preserved web_search_call items replay in order; tool results
// become function_call_output items.
func convertToDeepSeekResponsesInput(messages []types.Message, providerOptionsName string) ([]interface{}, []types.Warning, error) {
	input := []interface{}{}
	var warnings []types.Warning

	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			input = append(input, map[string]interface{}{"role": "system", "content": deepseekTextOnly(msg.Content)})

		case types.RoleUser:
			content := []interface{}{}
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					content = append(content, map[string]interface{}{"type": "input_text", "text": p.Text})
				case types.ImageContent:
					img, err := convertDeepSeekResponsesImagePart(imageContentAsFileContent(p), providerOptionsName)
					if err != nil {
						return nil, nil, err
					}
					content = append(content, img)
				case types.FileContent:
					if !deepseekIsImagePart(p) {
						warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("user message part type: %s", part.ContentType())})
						continue
					}
					img, err := convertDeepSeekResponsesImagePart(p, providerOptionsName)
					if err != nil {
						return nil, nil, err
					}
					content = append(content, img)
				default:
					warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("user message part type: %s", part.ContentType())})
				}
			}
			input = append(input, map[string]interface{}{"role": "user", "content": content})

		case types.RoleAssistant:
			hasCallParts := false
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					if p.Text != "" {
						input = append(input, map[string]interface{}{"role": "assistant", "content": p.Text})
					}
				case types.ReasoningContent:
					item := map[string]interface{}{
						"type":    "reasoning",
						"content": []interface{}{map[string]interface{}{"type": "reasoning_text", "text": p.Text}},
					}
					if id, ok := deepseekResponsesPartOption(p.ProviderOptions, p.ProviderMetadata, providerOptionsName, "itemId").(string); ok {
						item["id"] = id
					}
					input = append(input, item)
				case types.ToolCallContent:
					hasCallParts = true
					if p.ProviderExecuted {
						if call, ok := deepseekWebSearchCallItem(deepseekResponsesPartOption(p.ProviderOptions, p.ProviderMetadata, providerOptionsName, "webSearchCall")); ok {
							input = append(input, call)
						}
						continue
					}
					input = append(input, map[string]interface{}{
						"type":      "function_call",
						"call_id":   p.ToolCallID,
						"name":      p.ToolName,
						"arguments": deepseekResponsesCallArguments(p.Input, p.Arguments),
					})
				case types.ToolResultContent:
					// TS: a tool result in the assistant message is skipped.
				default:
					warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("assistant message part type: %s", part.ContentType())})
				}
			}
			// Go-only legacy form: calls carried on Message.ToolCalls.
			if !hasCallParts {
				for _, tc := range msg.ToolCalls {
					input = append(input, map[string]interface{}{
						"type":      "function_call",
						"call_id":   tc.ID,
						"name":      tc.ToolName,
						"arguments": deepseekResponsesCallArguments(tc.RawArguments, tc.Arguments),
					})
				}
			}

		case types.RoleTool:
			for _, part := range msg.Content {
				tr, ok := part.(types.ToolResultContent)
				if !ok {
					continue // TS: tool-approval-response parts are skipped
				}
				output, outputWarnings, err := deepseekResponsesToolOutput(tr, providerOptionsName)
				if err != nil {
					return nil, nil, err
				}
				warnings = append(warnings, outputWarnings...)
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": tr.ToolCallID,
					"output":  output,
				})
			}
		}
	}
	return input, warnings, nil
}

// deepseekResponsesPartOption reads providerOptions[name][key] (TS
// part.providerOptions?.[name]?.<key>); a replayed model output part's
// providerMetadata is read too, as Go callers resend parts as returned.
func deepseekResponsesPartOption(providerOptions map[string]interface{}, providerMetadata json.RawMessage, name, key string) interface{} {
	if opts, ok := providerutils.NormalizeProviderOptionsObject(providerOptions[name]); ok {
		if v, present := opts[key]; present {
			return v
		}
		return nil
	}
	if len(providerMetadata) > 0 {
		var meta map[string]map[string]interface{}
		if json.Unmarshal(providerMetadata, &meta) == nil {
			return meta[name][key]
		}
	}
	return nil
}

// deepseekWebSearchCallItem is TS isWebSearchCallItem: an object whose type
// is web_search_call, sent as given.
func deepseekWebSearchCallItem(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	if !ok || m["type"] != "web_search_call" {
		return nil, false
	}
	return m, true
}

// deepseekResponsesCallArguments is TS `typeof part.input === 'string' ?
// part.input : JSON.stringify(part.input)`.
func deepseekResponsesCallArguments(input string, args map[string]interface{}) string {
	if input != "" {
		return input
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	b, _ := jsonstr.Marshal(args)
	return string(b)
}

// deepseekResponsesToolOutput is the function_call_output value of a tool
// result: text and error-text as given, execution-denied as its reason, json
// and error-json as JSON text, content as input_text / input_image parts.
func deepseekResponsesToolOutput(tr types.ToolResultContent, providerOptionsName string) (interface{}, []types.Warning, error) {
	if tr.Output == nil {
		return deepseekStringifyResult(tr.Result), nil, nil
	}
	switch tr.Output.Type {
	case types.ToolResultOutputText, types.ToolResultOutputErrorText:
		return deepseekStringValue(tr.Output.Value), nil, nil
	case types.ToolResultOutputExecutionDenied:
		return tr.Output.ReasonOr("Tool call execution denied."), nil, nil
	case types.ToolResultOutputContent:
		var warnings []types.Warning
		parts := []interface{}{}
		for _, block := range tr.Output.Content {
			switch b := block.(type) {
			case types.TextContentBlock:
				parts = append(parts, map[string]interface{}{"type": "input_text", "text": b.Text})
			case types.FileContentBlock:
				if !deepseekIsImageBlock(b) {
					warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("tool result content part type: %s", b.ToolResultContentType())})
					continue
				}
				fc := types.FileContent{FileData: b.FileData, Data: b.Data, MediaType: b.MediaType, URL: b.URL, Reference: b.Reference, ProviderOptions: b.ProviderOptions}
				img, err := convertDeepSeekResponsesImagePart(fc, providerOptionsName)
				if err != nil {
					return nil, nil, err
				}
				parts = append(parts, img)
			case types.ImageContentBlock:
				fc := types.FileContent{Data: b.Data, MediaType: b.MediaType, ProviderOptions: b.ProviderOptions}
				img, err := convertDeepSeekResponsesImagePart(fc, providerOptionsName)
				if err != nil {
					return nil, nil, err
				}
				parts = append(parts, img)
			default:
				warnings = append(warnings, types.Warning{Type: "unsupported", Feature: fmt.Sprintf("tool result content part type: %s", block.ToolResultContentType())})
			}
		}
		return parts, warnings, nil
	default: // json, error-json (and the Go-only error)
		b, _ := jsonstr.Marshal(tr.Output.Value)
		return string(b), nil, nil
	}
}

// convertDeepSeekResponsesImagePart is TS convertImagePart: a provider
// reference is a file_id; otherwise the file part options are parsed
// (fileData is unsupported), a URL (at most 8192 characters) is sent as
// image_url with its detail, and inline data as a data URL of a supported
// image type.
func convertDeepSeekResponsesImagePart(f types.FileContent, providerOptionsName string) (map[string]interface{}, error) {
	if f.FileData.Type == types.FileDataTypeReference {
		ref, err := providerutils.ResolveProviderReference(f.FileData.Reference, "deepseek")
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"type": "input_image", "file_id": ref}, nil
	}

	fileOpts, err := providerutils.ParseProviderOptions(providerOptionsName, f.ProviderOptions, deepseekFilePartOptionsSchema)
	if err != nil {
		return nil, err
	}
	if fileData, _ := fileOpts["fileData"].(bool); fileData {
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "DeepSeek `fileData` image parts with the Responses API"}
	}
	detail, hasDetail := fileOpts["imageDetail"]

	if url := deepseekFileURL(f); url != "" {
		if len(url) > deepseekMaxImageURLLength {
			return nil, &providererrors.InvalidPromptError{Message: "DeepSeek image URLs must not exceed 8192 characters."}
		}
		part := map[string]interface{}{"type": "input_image", "image_url": url}
		if hasDetail && detail != nil {
			part["detail"] = detail
		}
		return part, nil
	}
	if f.Reference != "" {
		// Go-only legacy plain-string reference.
		return map[string]interface{}{"type": "input_image", "file_id": f.Reference}, nil
	}
	if f.FileData.Type != "" && f.FileData.Type != types.FileDataTypeData {
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("DeepSeek image data type %s", f.FileData.Type)}
	}

	mediaType := firstNonEmptyDS(f.MediaType, f.MimeType, f.FileData.MediaType)
	var resolved string
	if s := deepseekFileDataString(f); s != "" {
		resolved, err = deepseekResolveBase64MediaType(mediaType, s)
	} else {
		resolved, err = prompt.ResolveFullMediaType(mediaType, deepseekFileBytes(f), true)
	}
	if err != nil {
		return nil, err
	}
	resolved = strings.ToLower(resolved)
	if !deepseekResponsesImageMediaTypes[resolved] {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("DeepSeek image media type %s", resolved),
			Message:       "DeepSeek supports JPEG, PNG, GIF, and WebP image inputs.",
		}
	}
	wire := resolved
	if wire == "image/jpg" {
		wire = "image/jpeg"
	}
	payload := deepseekFileDataString(f)
	if payload == "" {
		payload = base64.StdEncoding.EncodeToString(deepseekFileBytes(f))
	}
	part := map[string]interface{}{"type": "input_image", "image_url": "data:" + wire + ";base64," + payload}
	if hasDetail && detail != nil {
		part["detail"] = detail
	}
	return part, nil
}
