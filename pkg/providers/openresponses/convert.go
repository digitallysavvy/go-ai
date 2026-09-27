package openresponses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ConvertToOpenResponsesInput converts AI SDK messages to Open Responses
// format using the OpenAI provider-reference key. Provider request paths should
// call ConvertToOpenResponsesInputForProvider so missing provider references
// can be returned as errors instead of ignored for compatibility.
func ConvertToOpenResponsesInput(messages []types.Message, system string) (interface{}, string, []types.Warning) {
	input, instructions, warnings, _ := ConvertToOpenResponsesInputForProvider(messages, system, "openai")
	return input, instructions, warnings
}

// ConvertToOpenResponsesInputForProvider converts messages and resolves
// provider references using the active provider key. It mirrors the TypeScript
// SDK provider adapters, which throw NoSuchProviderReferenceError when a file
// reference does not contain an ID for the current provider.
func ConvertToOpenResponsesInputForProvider(messages []types.Message, system string, providerName string) (interface{}, string, []types.Warning, error) {
	return ConvertToOpenResponsesInputForProviderStrict(messages, system, providerName, false)
}

// ConvertToOpenResponsesInputForProviderStrict is the full-featured entry
// point used by the language model. strictResponseInput mirrors the
// TypeScript SDK's `strictResponseInput` provider setting (see
// Config.StrictResponseInput / item 5 of the OR-CORE port): when true,
// assistant text without a known item ID is serialized as a plain string
// message, while assistant text with a known item ID is replayed as a
// complete output-text output item.
func ConvertToOpenResponsesInputForProviderStrict(messages []types.Message, system string, providerName string, strictResponseInput bool, extOpts ...openResponsesExtensionOptions) (interface{}, string, []types.Warning, error) {
	var input []interface{}
	var warnings []types.Warning
	var systemMessages []string
	if providerName == "" {
		providerName = "openai"
	}
	var extensionOptions openResponsesExtensionOptions
	if len(extOpts) > 0 {
		extensionOptions = extOpts[0]
	}

	// Collect system messages
	if system != "" {
		systemMessages = append(systemMessages, system)
	}

	// Convert each message
	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			// System messages become instructions - extract text from content parts
			for _, part := range msg.Content {
				if textContent, ok := part.(types.TextContent); ok {
					systemMessages = append(systemMessages, textContent.Text)
				}
			}

		case types.RoleUser:
			userContent, err := convertUserContent(msg.Content, &warnings, providerName)
			if err != nil {
				return nil, "", warnings, err
			}
			input = append(input, MessageItem{
				Type:    "message",
				Role:    "user",
				Content: userContent,
			})

		case types.RoleAssistant:
			input = append(input, convertAssistantContent(msg.Content, providerName, strictResponseInput, extensionOptions)...)

		case types.RoleTool:
			// Convert tool results
			toolResults, err := convertToolResults(msg.Content, &warnings, providerName)
			if err != nil {
				return nil, "", warnings, err
			}
			input = append(input, toolResults...)
		}
	}

	// Combine system messages into instructions
	var instructions string
	if len(systemMessages) > 0 {
		instructions = strings.Join(systemMessages, "\n")
	}

	return input, instructions, warnings, nil
}

// convertUserContent converts user message content to Open Responses format
func convertUserContent(content []types.ContentPart, warnings *[]types.Warning, providerName string) ([]interface{}, error) {
	var result []interface{}

	for _, part := range content {
		switch p := part.(type) {
		case types.TextContent:
			result = append(result, InputTextContent{
				Type: "input_text",
				Text: p.Text,
			})

		case types.ImageContent:
			imageURL := convertImageContentToURL(p, warnings)
			if imageURL != "" {
				result = append(result, InputImageContent{
					Type:     "input_image",
					ImageURL: imageURL,
					Detail:   openResponsesImageDetail(p.ProviderOptions, nil, providerName),
				})
			}

		case types.FileContent:
			file, err := normalizeFileContentData(p)
			if err != nil {
				return nil, err
			}
			mediaType := file.MediaType
			if strings.HasPrefix(mediaType, "image/") || mediaType == "image" {
				imageURL := convertFileToImageURL(file, warnings)
				if imageURL != "" {
					result = append(result, InputImageContent{
						Type:     "input_image",
						ImageURL: imageURL,
						Detail:   openResponsesImageDetail(file.ProviderOptions, nil, providerName),
					})
				}
				continue
			}
			switch {
			case file.URL != "":
				result = append(result, InputFileContent{Type: "input_file", FileURL: file.URL})
			case file.Reference != "":
				result = append(result, InputFileContent{Type: "input_file", FileID: file.Reference})
			case len(file.Data) > 0:
				filename := file.Filename
				if filename == "" {
					filename = "data"
				}
				result = append(result, InputFileContent{
					Type:     "input_file",
					FileData: fmt.Sprintf("data:%s;base64,%s", mediaTypeOrDefault(file.MediaType), base64.StdEncoding.EncodeToString(file.Data)),
					Filename: filename,
				})
			case file.Text != "":
				result = append(result, InputTextContent{Type: "input_text", Text: file.Text})
			}
		}
	}

	return result, nil
}

// convertImageContentToURL converts ImageContent to a data URL or returns the URL
func convertImageContentToURL(img types.ImageContent, warnings *[]types.Warning) string {
	// Check for URL first
	if img.URL != "" {
		return img.URL
	}

	// Convert data to base64 data URL
	if len(img.Image) > 0 {
		mediaType := img.MimeType
		if mediaType == "" {
			mediaType = "image/jpeg"
		}
		dataStr := base64.StdEncoding.EncodeToString(img.Image)
		return fmt.Sprintf("data:%s;base64,%s", mediaType, dataStr)
	}

	return ""
}

// convertFileToImageURL converts FileContent to an image data URL
func convertFileToImageURL(file types.FileContent, warnings *[]types.Warning) string {
	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = file.MimeType
	}
	if mediaType == "" || mediaType == "image/*" {
		mediaType = "image/jpeg"
	}

	if file.URL != "" {
		return file.URL
	}

	if file.Reference != "" {
		return file.Reference
	}

	if file.Text != "" {
		return fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString([]byte(file.Text)))
	}

	if len(file.Data) > 0 {
		dataStr := base64.StdEncoding.EncodeToString(file.Data)
		return fmt.Sprintf("data:%s;base64,%s", mediaType, dataStr)
	}

	return ""
}

func normalizeFileContentData(file types.FileContent) (types.FileContent, error) {
	if file.FileData.IsZero() {
		return file, nil
	}
	switch file.FileData.Type {
	case types.FileDataTypeData:
		file.Data = file.FileData.Data
	case types.FileDataTypeURL:
		file.URL = file.FileData.URL
	case types.FileDataTypeReference:
		return types.FileContent{}, fmt.Errorf("openresponses: file parts with provider references are not supported")
	case types.FileDataTypeText:
		return types.FileContent{}, fmt.Errorf("openresponses: text file parts are not supported")
	}
	if file.FileData.MediaType != "" && file.MediaType == "" {
		file.MediaType = file.FileData.MediaType
	}
	return file, nil
}

func mediaTypeOrDefault(mediaType string) string {
	if mediaType == "" {
		return "application/octet-stream"
	}
	return mediaType
}

// convertAssistantContent converts assistant message content to an ordered
// list of Open Responses input items, mirroring the TypeScript SDK's
// convertToOpenResponsesInput assistant branch: an in-progress assistant
// message is flushed whenever a reasoning or tool-call item interrupts it, or
// whenever a text part's itemId boundary changes, so reasoning/text/tool-call
// ordering and per-item boundaries survive a round trip (OR-CORE item 3).
// openResponsesExtensionOptions carries the registry and declared tools
// needed to encode/decode Open Responses extension content on replay (row
// 9a68261, OR-EXT). The zero value disables all extension handling, so
// existing callers that don't pass it see unchanged behavior.
type openResponsesExtensionOptions struct {
	Registry *ExtensionRegistry
	Tools    []types.Tool
}

func convertAssistantContent(content []types.ContentPart, providerName string, strictResponseInput bool, extOpts ...openResponsesExtensionOptions) []interface{} {
	var opts openResponsesExtensionOptions
	if len(extOpts) > 0 {
		opts = extOpts[0]
	}
	var items []interface{}
	var assistantContent []OutputTextContent
	var assistantMessageID string
	handledExtensionItemIDs := map[string]bool{}

	flush := func() {
		if len(assistantContent) == 0 {
			return
		}
		switch {
		case strictResponseInput && assistantMessageID == "":
			var text strings.Builder
			for _, part := range assistantContent {
				text.WriteString(part.Text)
			}
			items = append(items, MessageItem{Type: "message", Role: "assistant", Content: text.String()})
		case strictResponseInput:
			parts := make([]interface{}, 0, len(assistantContent))
			for _, part := range assistantContent {
				if part.Annotations == nil {
					part.Annotations = []Annotation{}
				}
				if part.Logprobs == nil {
					part.Logprobs = []interface{}{}
				}
				parts = append(parts, part)
			}
			items = append(items, MessageItem{
				ID:      assistantMessageID,
				Type:    "message",
				Status:  "completed",
				Role:    "assistant",
				Content: parts,
			})
		default:
			parts := make([]interface{}, 0, len(assistantContent))
			for _, part := range assistantContent {
				parts = append(parts, part)
			}
			item := MessageItem{Type: "message", Role: "assistant", Content: parts}
			if assistantMessageID != "" {
				item.ID = assistantMessageID
			}
			items = append(items, item)
		}
		assistantContent = nil
		assistantMessageID = ""
	}

	for _, part := range content {
		switch p := part.(type) {
		case types.ReasoningContent:
			// Always emit reasoning as a top-level reasoning input item (OR-CORE
			// item 4/5) — do not gate on EncryptedContent being present; the TS
			// SDK never drops a reasoning part here.
			flush()

			data := openResponsesProviderData(p.ProviderOptions, p.ProviderMetadata, providerName)
			itemID, _ := data["itemId"].(string)
			summary := openResponsesParseSummaryParts(data["reasoningSummary"])
			if summary == nil {
				summary = []SummaryPart{}
			}
			reasoningContent, hasReasoningContentKey := openResponsesParseReasoningTextParts(data)
			encryptedContent, _ := data["reasoningEncryptedContent"].(string)
			if encryptedContent == "" {
				encryptedContent = p.EncryptedContent
			}

			reasoningItem := ReasoningInputItem{Type: "reasoning", Summary: summary}
			if itemID != "" {
				reasoningItem.ID = itemID
			}
			switch {
			case reasoningContent != nil:
				reasoningItem.Content = reasoningContent
			case !hasReasoningContentKey && p.Text != "":
				reasoningItem.Content = []ReasoningTextPart{{Type: "reasoning_text", Text: p.Text}}
			}
			if encryptedContent != "" {
				reasoningItem.EncryptedContent = encryptedContent
			}

			if reasoningItem.ID != "" && len(items) > 0 {
				if prev, ok := items[len(items)-1].(ReasoningInputItem); ok && prev.ID == reasoningItem.ID {
					if reasoningItem.Content != nil {
						prev.Content = append(prev.Content, reasoningItem.Content...)
						items[len(items)-1] = prev
					}
					continue
				}
			}
			items = append(items, reasoningItem)

		case types.TextContent:
			data := openResponsesProviderData(p.ProviderOptions, p.ProviderMetadata, providerName)
			itemID, _ := data["itemId"].(string)
			annotations := openResponsesParseAnnotations(data["annotations"])

			if len(assistantContent) > 0 && assistantMessageID != itemID {
				flush()
			}
			assistantMessageID = itemID
			assistantContent = append(assistantContent, OutputTextContent{
				Type:        "output_text",
				Text:        p.Text,
				Annotations: annotations,
			})

		case types.CustomContent:
			// Row 9a68261 (OR-EXT): replay carrier preserving an extension
			// item's original bytes verbatim, added alongside its decoded
			// content parts by decodeExtensionItem.
			if p.Kind == extensionReplayKind {
				if item, itemID := extensionReplayInputItem(p.ProviderMetadata, providerName); item != nil {
					flush()
					items = append(items, item)
					if itemID != "" {
						handledExtensionItemIDs[itemID] = true
					}
				}
			}

		case types.ToolCallContent:
			if p.ProviderExecuted {
				// Row 9a68261 (OR-EXT): a provider-executed call from a
				// registered extension replays via its preserved raw item
				// (handled above, once, via the item's replay carrier) or,
				// lacking one, via the extension's EncodeInputItem.
				extensionID, itemID := extensionReferenceInfo(p.ProviderMetadata, providerName)
				if itemID != "" && handledExtensionItemIDs[itemID] {
					continue
				}
				if encoded := encodeExtensionInputItems(extensionID, p, opts); len(encoded) > 0 {
					flush()
					items = append(items, encoded...)
				}
				continue
			}
			flush()

			data := openResponsesProviderData(p.ProviderOptions, p.ProviderMetadata, providerName)
			itemID, _ := data["itemId"].(string)
			namespace, _ := data["namespace"].(string)

			item := FunctionCallItem{
				Type:      "function_call",
				CallID:    p.ToolCallID,
				Name:      p.ToolName,
				Arguments: serializeToolCallArguments(p),
				Namespace: namespace,
			}
			if itemID != "" {
				item.ID = itemID
			}
			items = append(items, item)

		case types.ToolResultContent:
			// Row 9a68261 (OR-EXT): a provider-executed result embedded in
			// the assistant's own turn (e.g. a synchronously-resolved
			// extension tool), mirroring the ToolCallContent handling above.
			// Non-extension provider-executed results have no other replay
			// path in this package and are silently skipped, unchanged from
			// before this feature existed.
			if !p.ProviderExecuted {
				continue
			}
			extensionID, itemID := extensionReferenceInfo(p.ProviderMetadata, providerName)
			if itemID != "" && handledExtensionItemIDs[itemID] {
				continue
			}
			if encoded := encodeExtensionInputItems(extensionID, p, opts); len(encoded) > 0 {
				flush()
				items = append(items, encoded...)
			}
		}
	}

	flush()

	return items
}

func serializeToolCallArguments(toolCall types.ToolCallContent) string {
	if toolCall.Input != "" {
		if json.Valid([]byte(toolCall.Input)) {
			return toolCall.Input
		}
		data, err := json.Marshal(toolCall.Input)
		if err == nil {
			return string(data)
		}
	}
	if toolCall.Arguments != nil {
		data, err := json.Marshal(toolCall.Arguments)
		if err == nil {
			return string(data)
		}
	}
	return "{}"
}

// openResponsesProviderKeys returns the ordered, de-duplicated set of
// provider-options/metadata keys to check for Open Responses data, mirroring
// the fallback set the language model already accepts elsewhere in this
// package (the active provider name, then the historical aliases).
func openResponsesProviderKeys(providerName string) []string {
	candidates := []string{providerName, "openai", "openResponses", "open-responses"}
	seen := make(map[string]bool, len(candidates))
	keys := make([]string, 0, len(candidates))
	for _, key := range candidates {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

// openResponsesProviderData extracts the provider-scoped metadata map for a
// content part, checking ProviderOptions first (the input direction; set by
// callers replaying history) and falling back to ProviderMetadata (raw JSON
// this provider attaches to model output), across all recognized provider
// keys. Mirrors the TS SDK's getProviderData helper.
func openResponsesProviderData(providerOptions map[string]interface{}, providerMetadata json.RawMessage, providerName string) map[string]interface{} {
	for _, key := range openResponsesProviderKeys(providerName) {
		if nested, ok := asStringMap(providerOptions[key]); ok {
			return nested
		}
	}
	if len(providerMetadata) > 0 {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(providerMetadata, &values); err == nil {
			for _, key := range openResponsesProviderKeys(providerName) {
				raw, ok := values[key]
				if !ok {
					continue
				}
				var nested map[string]interface{}
				if err := json.Unmarshal(raw, &nested); err == nil {
					return nested
				}
			}
		}
	}
	return nil
}

func asStringMap(value interface{}) (map[string]interface{}, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed, true
	case nil:
		return nil, false
	default:
		rv := reflect.ValueOf(value)
		if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
			return nil, false
		}
		out := make(map[string]interface{}, rv.Len())
		for _, k := range rv.MapKeys() {
			out[k.String()] = rv.MapIndex(k).Interface()
		}
		return out, true
	}
}

// openResponsesImageDetail resolves the `detail` field for input images,
// defaulting to "auto" when unset or invalid (OR-CORE item 6). Mirrors TS
// getImageDetail(providerOptions[providerName].imageDetail).
func openResponsesImageDetail(providerOptions map[string]interface{}, providerMetadata json.RawMessage, providerName string) string {
	data := openResponsesProviderData(providerOptions, providerMetadata, providerName)
	if detail, ok := data["imageDetail"].(string); ok {
		switch detail {
		case "low", "high", "auto":
			return detail
		}
	}
	return "auto"
}

// openResponsesParseSummaryParts validates and converts a raw
// reasoningSummary provider-data value into SummaryPart entries. Returns nil
// when the value isn't a well-formed summary_text array (mirrors TS
// parseReasoningSummary, which returns undefined on any shape mismatch).
func openResponsesParseSummaryParts(value interface{}) []SummaryPart {
	list, ok := value.([]interface{})
	if !ok {
		return nil
	}
	out := make([]SummaryPart, 0, len(list))
	for _, entry := range list {
		m, ok := entry.(map[string]interface{})
		if !ok || m["type"] != "summary_text" {
			return nil
		}
		text, ok := m["text"].(string)
		if !ok {
			return nil
		}
		out = append(out, SummaryPart{Type: "summary_text", Text: text})
	}
	return out
}

// openResponsesParseReasoningTextParts validates and converts a raw
// reasoningContent provider-data value into ReasoningTextPart entries. The
// second return value reports whether the "reasoningContent" key was present
// at all (even if null/invalid), mirroring TS's `'reasoningContent' in
// providerData` check used to distinguish "no content recorded" from
// "content was explicitly empty".
func openResponsesParseReasoningTextParts(data map[string]interface{}) ([]ReasoningTextPart, bool) {
	value, hasKey := data["reasoningContent"]
	if !hasKey {
		return nil, false
	}
	list, ok := value.([]interface{})
	if !ok {
		return nil, true
	}
	out := make([]ReasoningTextPart, 0, len(list))
	for _, entry := range list {
		m, ok := entry.(map[string]interface{})
		if !ok || m["type"] != "reasoning_text" {
			return nil, true
		}
		text, ok := m["text"].(string)
		if !ok {
			return nil, true
		}
		out = append(out, ReasoningTextPart{Type: "reasoning_text", Text: text})
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, true
}

// openResponsesParseAnnotations validates and converts a raw annotations
// provider-data value into Annotation entries, mirroring TS
// parseOutputTextAnnotations / getOutputTextAnnotations: only well-formed
// url_citation entries are accepted, otherwise nil is returned.
func openResponsesParseAnnotations(value interface{}) []Annotation {
	list, ok := value.([]interface{})
	if !ok {
		return nil
	}
	out := make([]Annotation, 0, len(list))
	for _, entry := range list {
		m, ok := entry.(map[string]interface{})
		if !ok || m["type"] != "url_citation" {
			return nil
		}
		start, ok := asInt(m["start_index"])
		if !ok {
			return nil
		}
		end, ok := asInt(m["end_index"])
		if !ok {
			return nil
		}
		url, ok := m["url"].(string)
		if !ok {
			return nil
		}
		title, ok := m["title"].(string)
		if !ok {
			return nil
		}
		out = append(out, Annotation{Type: "url_citation", StartIndex: start, EndIndex: end, URL: url, Title: title})
	}
	return out
}

func asInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// convertToolResults converts tool results to Open Responses format
func convertToolResults(content []types.ContentPart, warnings *[]types.Warning, providerName string) ([]interface{}, error) {
	var results []interface{}

	for _, part := range content {
		if part.ContentType() == "tool-result" {
			if toolResult, ok := part.(types.ToolResultContent); ok {
				output, err := convertToolResultOutput(toolResult, warnings, providerName)
				if err != nil {
					return nil, err
				}

				results = append(results, FunctionCallOutputItem{
					Type:   "function_call_output",
					CallID: toolResult.ToolCallID,
					Output: output,
				})
			}
		}
	}

	return results, nil
}

// convertToolResultOutput converts tool result output to appropriate format
func convertToolResultOutput(toolResult types.ToolResultContent, warnings *[]types.Warning, providerName string) (interface{}, error) {
	if toolResult.Output != nil {
		return convertStructuredToolResultOutput(*toolResult.Output, warnings, providerName)
	}

	// If there's an error, return the error message
	if toolResult.Error != "" {
		return toolResult.Error, nil
	}

	// If result is nil, return empty string
	if toolResult.Result == nil {
		return "", nil
	}

	// Try to convert result to string if it's a simple type
	switch v := toolResult.Result.(type) {
	case string:
		return v, nil
	case int, int32, int64, float32, float64, bool:
		return fmt.Sprintf("%v", v), nil
	default:
		// For complex types, JSON encode
		jsonBytes, _ := json.Marshal(v)
		return string(jsonBytes), nil
	}
}

func convertStructuredToolResultOutput(output types.ToolResultOutput, warnings *[]types.Warning, providerName string) (interface{}, error) {
	switch output.Type {
	case types.ToolResultOutputText, types.ToolResultOutputError, types.ToolResultOutputErrorText, types.ToolResultOutputErrorJSON:
		if output.Value == nil {
			return "", nil
		}
		if value, ok := output.Value.(string); ok {
			return value, nil
		}
		return fmt.Sprintf("%v", output.Value), nil
	case types.ToolResultOutputExecutionDenied:
		if output.Reason != "" {
			return output.Reason, nil
		}
		return "Tool call execution denied.", nil
	case types.ToolResultOutputJSON:
		jsonBytes, _ := json.Marshal(output.Value)
		return string(jsonBytes), nil
	case types.ToolResultOutputContent:
		parts := make([]interface{}, 0, len(output.Content))
		for _, block := range output.Content {
			switch item := block.(type) {
			case types.TextContentBlock:
				parts = append(parts, InputTextContent{Type: "input_text", Text: item.Text})
			case types.ImageContentBlock:
				mediaType := item.MediaType
				if mediaType == "" {
					mediaType = "image/jpeg"
				}
				parts = append(parts, InputImageContent{
					Type:     "input_image",
					ImageURL: fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(item.Data)),
					Detail:   openResponsesImageDetail(item.ProviderOptions, nil, providerName),
				})
			case types.FileContentBlock:
				converted, ok, err := convertToolFileContentBlock(item, warnings, providerName)
				if err != nil {
					return nil, err
				}
				if ok {
					parts = append(parts, converted)
				}
			default:
				*warnings = append(*warnings, types.Warning{
					Type:    "other",
					Message: fmt.Sprintf("unsupported tool content part type: %s", block.ToolResultContentType()),
				})
			}
		}
		return parts, nil
	default:
		if output.Value == nil {
			return "", nil
		}
		jsonBytes, _ := json.Marshal(output.Value)
		return string(jsonBytes), nil
	}
}

func convertToolFileContentBlock(block types.FileContentBlock, warnings *[]types.Warning, providerName string) (interface{}, bool, error) {
	if block.FileData.Type == types.FileDataTypeReference || (block.FileData.Type == "" && len(block.FileData.Reference) > 0) || block.Reference != "" {
		*warnings = append(*warnings, types.Warning{
			Type:    "other",
			Message: "unsupported tool content part type: file with data type: reference",
		})
		return nil, false, nil
	}
	if block.FileData.Type == types.FileDataTypeText || (block.FileData.Type == "" && block.FileData.Text != "") || block.Text != "" {
		*warnings = append(*warnings, types.Warning{
			Type:    "other",
			Message: "unsupported tool content part type: file with data type: text",
		})
		return nil, false, nil
	}
	file, err := normalizeFileContentBlockData(block, providerName)
	if err != nil {
		return nil, false, err
	}
	mediaType := mediaTypeOrDefault(file.MediaType)
	if strings.HasPrefix(mediaType, "image/") || mediaType == "image" {
		detail := openResponsesImageDetail(block.ProviderOptions, nil, providerName)
		switch {
		case file.URL != "":
			return InputImageContent{Type: "input_image", ImageURL: file.URL, Detail: detail}, true, nil
		case file.Reference != "":
			return InputImageContent{Type: "input_image", ImageURL: file.Reference, Detail: detail}, true, nil
		case len(file.Data) > 0:
			return InputImageContent{
				Type:     "input_image",
				ImageURL: fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(file.Data)),
				Detail:   detail,
			}, true, nil
		case file.Text != "":
			return InputImageContent{
				Type:     "input_image",
				ImageURL: fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString([]byte(file.Text))),
				Detail:   detail,
			}, true, nil
		default:
			*warnings = append(*warnings, types.Warning{Type: "other", Message: "unsupported tool content part type: file"})
			return nil, false, nil
		}
	}

	switch {
	case file.URL != "":
		return InputFileContent{Type: "input_file", FileURL: file.URL}, true, nil
	case file.Reference != "":
		return InputFileContent{Type: "input_file", FileID: file.Reference}, true, nil
	case len(file.Data) > 0:
		filename := file.Filename
		if filename == "" {
			filename = "data"
		}
		return InputFileContent{
			Type:     "input_file",
			Filename: filename,
			FileData: fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(file.Data)),
		}, true, nil
	case file.Text != "":
		return InputTextContent{Type: "input_text", Text: file.Text}, true, nil
	default:
		*warnings = append(*warnings, types.Warning{Type: "other", Message: "unsupported tool content part type: file"})
		return nil, false, nil
	}
}

func normalizeFileContentBlockData(block types.FileContentBlock, providerName string) (types.FileContentBlock, error) {
	if block.FileData.IsZero() {
		return block, nil
	}
	switch block.FileData.Type {
	case types.FileDataTypeData:
		block.Data = block.FileData.Data
	case types.FileDataTypeURL:
		block.URL = block.FileData.URL
	case types.FileDataTypeReference:
		ref, err := providerutils.ResolveProviderReference(block.FileData.Reference, providerName)
		if err != nil {
			return types.FileContentBlock{}, err
		}
		block.Reference = ref
	case types.FileDataTypeText:
		block.Text = block.FileData.Text
	}
	if block.FileData.MediaType != "" && block.MediaType == "" {
		block.MediaType = block.FileData.MediaType
	}
	return block, nil
}
