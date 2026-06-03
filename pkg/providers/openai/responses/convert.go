package responses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertPromptToInput converts a types.Prompt to the Responses API input slice.
// systemMessageMode must be "system", "developer", or "remove".
//
// The resulting slice is suitable for use as the "input" field in a Responses
// API request body. Each element is one of:
//   - SystemMessage (system/developer role)
//   - UserMessage
//   - AssistantMessageItem
//   - FunctionCallItem
//   - FunctionCallOutputItem
func ConvertPromptToInput(prompt types.Prompt, systemMessageMode string) []interface{} {
	input, _ := ConvertPromptToInputWithOptions(prompt, systemMessageMode, ConvertOptions{PassThroughUnsupportedFiles: true})
	return input
}

// ConvertOptions controls provider-specific Responses API conversion behavior.
type ConvertOptions struct {
	PassThroughUnsupportedFiles bool
	HasPreviousResponseID       bool
	HasConversation             bool
	Store                       bool
	CustomToolNames             map[string]bool
	HasLocalShellTool           bool
	HasShellTool                bool
	HasApplyPatchTool           bool
	FileIDPrefixes              []string
	ProviderOptionsName         string
}

// ConvertPromptToInputWithOptions converts a prompt to Responses API input and
// validates file media types according to OpenAI Responses defaults.
func ConvertPromptToInputWithOptions(prompt types.Prompt, systemMessageMode string, opts ConvertOptions) ([]interface{}, error) {
	input := make([]interface{}, 0, len(prompt.Messages)+1)

	// Prepend system message when present and not suppressed.
	if prompt.System != "" && systemMessageMode != "remove" {
		input = append(input, SystemMessage{
			Role:    systemMessageMode,
			Content: prompt.System,
		})
	}

	for _, msg := range prompt.Messages {
		switch msg.Role {
		case types.RoleUser:
			userMessage, err := convertUserMessage(msg, opts)
			if err != nil {
				return nil, err
			}
			input = append(input, userMessage)
		case types.RoleAssistant:
			input = append(input, convertAssistantItems(msg, opts)...)
		case types.RoleTool:
			input = append(input, convertToolItems(msg, opts)...)
		}
	}

	return input, nil
}

// convertUserMessage maps a user-role Message to a UserMessage.
// Responses uses a typed content array for user messages, matching the
// TypeScript SDK converter even for single text messages.
func convertUserMessage(msg types.Message, opts ConvertOptions) (UserMessage, error) {
	providerName := openAIProviderOptionsName(opts)
	parts := make([]interface{}, 0, len(msg.Content))
	for index, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			parts = append(parts, UserTextPart{Type: "input_text", Text: p.Text})
		case types.ImageContent:
			imageURL := p.URL
			if imageURL == "" && len(p.Image) > 0 {
				imageURL = fmt.Sprintf("data:%s;base64,%s",
					p.MimeType, base64.StdEncoding.EncodeToString(p.Image))
			}
			if imageURL != "" {
				parts = append(parts, UserImageURLPart{
					Type:     "input_image",
					ImageURL: imageURL,
					Detail:   openAIResponsesImageDetail(p.ProviderOptions, providerName),
				})
			}
		case types.FileContent:
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
			case types.FileDataTypeURL:
				fileURL := firstNonEmpty(p.FileData.URL, p.URL)
				if fileURL == "" {
					continue
				}
				if isImageMediaType(mediaType) {
					parts = append(parts, UserImageURLPart{
						Type:     "input_image",
						ImageURL: fileURL,
						Detail:   openAIResponsesImageDetail(p.ProviderOptions, providerName),
					})
				} else {
					if err := validateResponsesFileMediaType(mediaType, opts.PassThroughUnsupportedFiles, providerName); err != nil {
						return UserMessage{}, err
					}
					parts = append(parts, UserFilePart{Type: "input_file", FileURL: fileURL})
				}
			case types.FileDataTypeReference:
				reference := firstNonEmpty(providerReferenceString(p.FileData.Reference, providerName), p.Reference)
				if reference == "" {
					continue
				}
				if isImageMediaType(mediaType) {
					part := map[string]interface{}{
						"type":    "input_image",
						"file_id": reference,
					}
					if detail := openAIResponsesImageDetail(p.ProviderOptions, providerName); detail != "" {
						part["detail"] = detail
					}
					parts = append(parts, part)
				} else {
					parts = append(parts, map[string]interface{}{"type": "input_file", "file_id": reference})
				}
			case types.FileDataTypeText:
				return UserMessage{}, fmt.Errorf("openai.responses: text file parts are not supported")
			case types.FileDataTypeData:
				if fileID := openAIFileIDFromDataString(p, opts.FileIDPrefixes); fileID != "" {
					if isImageMediaType(mediaType) {
						part := map[string]interface{}{
							"type":    "input_image",
							"file_id": fileID,
						}
						if detail := openAIResponsesImageDetail(p.ProviderOptions, providerName); detail != "" {
							part["detail"] = detail
						}
						parts = append(parts, part)
					} else {
						parts = append(parts, map[string]interface{}{"type": "input_file", "file_id": fileID})
					}
					continue
				}
				data := openAIFileDataBase64(p)
				if data == "" {
					continue
				}
				mediaType = resolveOpenAIFileDataMediaType(mediaType, p)
				fileData := fmt.Sprintf("data:%s;base64,%s", mediaType, data)
				if isImageMediaType(mediaType) {
					parts = append(parts, UserImageURLPart{
						Type:     "input_image",
						ImageURL: fileData,
						Detail:   openAIResponsesImageDetail(p.ProviderOptions, providerName),
					})
				} else {
					if err := validateResponsesFileMediaType(mediaType, opts.PassThroughUnsupportedFiles, providerName); err != nil {
						return UserMessage{}, err
					}
					part := map[string]interface{}{"type": "input_file", "file_data": fileData}
					if p.Filename != "" {
						part["filename"] = p.Filename
					} else if mediaType == "application/pdf" {
						part["filename"] = fmt.Sprintf("part-%d.pdf", index)
					} else {
						part["filename"] = fmt.Sprintf("part-%d", index)
					}
					parts = append(parts, part)
				}
			}
		}
	}

	return UserMessage{Role: "user", Content: parts}, nil
}

func openAIFileMediaType(part types.FileContent) string {
	return firstNonEmpty(part.MediaType, part.MimeType, part.FileData.MediaType)
}

func resolveOpenAIFileDataMediaType(mediaType string, part types.FileContent) string {
	if mediaType != "image/*" {
		return mediaType
	}
	data := part.FileData.Data
	if len(data) == 0 && part.FileData.DataString != "" {
		if decoded, err := types.DecodeFileDataString(part.FileData.DataString); err == nil {
			data = decoded
		}
	}
	if len(data) == 0 {
		data = part.Data
	}
	if len(data) == 0 {
		return mediaType
	}
	detected := http.DetectContentType(data)
	if strings.HasPrefix(detected, "image/") {
		return detected
	}
	return mediaType
}

func openAIFileDataBase64(part types.FileContent) string {
	if part.FileData.DataString != "" {
		return part.FileData.DataString
	}
	if len(part.FileData.Data) > 0 {
		return base64.StdEncoding.EncodeToString(part.FileData.Data)
	}
	if len(part.Data) > 0 {
		return base64.StdEncoding.EncodeToString(part.Data)
	}
	return ""
}

func openAIFileIDFromDataString(part types.FileContent, prefixes []string) string {
	if part.FileData.DataString == "" || len(prefixes) == 0 {
		return ""
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(part.FileData.DataString, prefix) {
			return part.FileData.DataString
		}
	}
	return ""
}

func isImageMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "image/") || mediaType == "image"
}

func validateResponsesFileMediaType(mediaType string, passThroughUnsupportedFiles bool, providerName string) error {
	if passThroughUnsupportedFiles || mediaType == "" || mediaType == "application/pdf" {
		return nil
	}
	if providerName == "" {
		providerName = "openai"
	}
	return fmt.Errorf("openai.responses: unsupported file media type %q; set providerOptions.%s.passThroughUnsupportedFiles to true to pass it through", mediaType, providerName)
}

// convertAssistantItems maps assistant-role content parts and ToolCalls to
// Responses API input items.
func convertAssistantItems(msg types.Message, opts ConvertOptions) []interface{} {
	items := make([]interface{}, 0, 1+len(msg.ToolCalls))
	providerName := openAIProviderOptionsName(opts)

	reasoningItems := map[string]map[string]interface{}{}
	toolCallContentIDs := map[string]bool{}
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			if item := convertAssistantTextItem(p, opts); item != nil {
				items = append(items, item)
			}
		case types.ReasoningContent:
			if item := convertReasoningItem(p, opts, reasoningItems); item != nil {
				items = append(items, item)
			}
		case types.ToolCallContent:
			toolCallContentIDs[p.ToolCallID] = true
			if item := convertAssistantToolCallContentItem(p, opts); item != nil {
				items = append(items, item)
			}
		case *types.ToolCallContent:
			if p != nil {
				toolCallContentIDs[p.ToolCallID] = true
				if item := convertAssistantToolCallContentItem(*p, opts); item != nil {
					items = append(items, item)
				}
			}
		case types.ToolResultContent:
			if item := convertAssistantToolResultItem(p, opts); item != nil {
				items = append(items, item)
			}
		case *types.ToolResultContent:
			if p != nil {
				if item := convertAssistantToolResultItem(*p, opts); item != nil {
					items = append(items, item)
				}
			}
		case types.CustomContent:
			if item := convertAssistantCustomItem(p, opts); item != nil {
				items = append(items, item)
			}
		case *types.CustomContent:
			if p != nil {
				if item := convertAssistantCustomItem(*p, opts); item != nil {
					items = append(items, item)
				}
			}
		}
	}
	if !opts.Store {
		items = filterReasoningItemsWithoutEncryptedContent(items)
	}

	// Each ToolCall on the message becomes a function_call item.
	for _, tc := range msg.ToolCalls {
		if toolCallContentIDs[tc.ID] {
			continue
		}
		itemID := openAIItemID(tc.ProviderMetadata, providerName)
		if opts.HasConversation && itemID != "" {
			continue
		}
		if item, handled := convertAssistantToolCallItem(tc, itemID, opts); handled {
			if item != nil {
				items = append(items, item)
			}
			continue
		}
		if opts.Store && itemID != "" {
			if opts.HasPreviousResponseID {
				continue
			}
			items = append(items, map[string]interface{}{
				"type": "item_reference",
				"id":   itemID,
			})
			continue
		}
		args := tc.Arguments
		if args == nil {
			args = map[string]interface{}{}
		}
		argsJSON, _ := json.Marshal(args)
		namespace := ""
		if openaiMeta, ok := tc.ProviderMetadata[providerName].(map[string]interface{}); ok {
			if rawNS, ok := openaiMeta["namespace"].(string); ok {
				namespace = rawNS
			}
		}
		items = append(items, FunctionCallItem{
			Type:      "function_call",
			ID:        itemID,
			CallID:    tc.ID,
			Name:      tc.ToolName,
			Namespace: namespace,
			Arguments: string(argsJSON),
		})
	}

	return items
}

func convertAssistantToolCallContentItem(part types.ToolCallContent, opts ConvertOptions) interface{} {
	providerName := openAIProviderOptionsName(opts)
	metadata := toolCallContentProviderMetadata(part)
	itemID := openAIItemID(metadata, providerName)
	if opts.HasConversation && itemID != "" {
		return nil
	}
	arguments, genericArguments := toolCallContentArguments(part)
	tc := types.ToolCall{
		ID:               part.ToolCallID,
		ToolName:         part.ToolName,
		Arguments:        arguments,
		RawArguments:     part.Input,
		ProviderExecuted: part.ProviderExecuted,
		ProviderMetadata: metadata,
	}
	if item, handled := convertAssistantToolCallItem(tc, itemID, opts); handled {
		return item
	}
	if opts.Store && itemID != "" {
		if opts.HasPreviousResponseID {
			return nil
		}
		return map[string]interface{}{
			"type": "item_reference",
			"id":   itemID,
		}
	}
	namespace := ""
	if openaiMeta, ok := tc.ProviderMetadata[providerName].(map[string]interface{}); ok {
		if rawNS, ok := openaiMeta["namespace"].(string); ok {
			namespace = rawNS
		}
	}
	return FunctionCallItem{
		Type:      "function_call",
		ID:        itemID,
		CallID:    tc.ID,
		Name:      tc.ToolName,
		Namespace: namespace,
		Arguments: genericArguments,
	}
}

func toolCallContentArguments(part types.ToolCallContent) (map[string]interface{}, string) {
	if part.Arguments != nil {
		argsJSON, _ := json.Marshal(part.Arguments)
		return part.Arguments, string(argsJSON)
	}
	if part.Input == "" {
		return map[string]interface{}{}, "{}"
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(part.Input), &parsed); err != nil {
		argsJSON, _ := json.Marshal(part.Input)
		return map[string]interface{}{}, string(argsJSON)
	}
	if args, ok := parsed.(map[string]interface{}); ok {
		argsJSON, _ := json.Marshal(args)
		return args, string(argsJSON)
	}
	argsJSON, _ := json.Marshal(parsed)
	return map[string]interface{}{}, string(argsJSON)
}

func convertAssistantToolCallItem(tc types.ToolCall, itemID string, opts ConvertOptions) (interface{}, bool) {
	toolName := normalizeOpenAIToolName(tc.ToolName)
	if toolName == "tool_search" {
		if opts.Store && itemID != "" {
			return map[string]interface{}{"type": "item_reference", "id": itemID}, true
		}
		arguments := tc.Arguments
		rawArguments, _ := json.Marshal(arguments)
		var callID *string
		execution := "server"
		if rawCallID, ok := arguments["call_id"].(string); ok && rawCallID != "" {
			callID = &rawCallID
			execution = "client"
		}
		if nested, ok := arguments["arguments"]; ok {
			rawArguments, _ = json.Marshal(nested)
		}
		return ToolSearchCallItem{
			Type:      "tool_search_call",
			ID:        itemID,
			Status:    "completed",
			Execution: execution,
			CallID:    callID,
			Arguments: rawArguments,
		}, true
	}
	if tc.ProviderExecuted {
		if opts.Store && itemID != "" {
			return map[string]interface{}{"type": "item_reference", "id": itemID}, true
		}
		return nil, true
	}
	if opts.Store && itemID != "" {
		if opts.HasPreviousResponseID {
			return nil, true
		}
		return map[string]interface{}{"type": "item_reference", "id": itemID}, true
	}
	if opts.HasLocalShellTool && toolName == "local_shell" {
		action := localShellActionFromArgs(tc.Arguments)
		return LocalShellCall{
			Type:   "local_shell_call",
			ID:     itemID,
			CallID: tc.ID,
			Action: action,
		}, true
	}
	if opts.HasShellTool && toolName == "shell" {
		return ShellCall{
			Type:   "shell_call",
			ID:     itemID,
			CallID: tc.ID,
			Status: "completed",
			Action: shellActionFromArgs(tc.Arguments),
		}, true
	}
	if opts.HasApplyPatchTool && toolName == "apply_patch" {
		callID := stringArg(tc.Arguments, "callId")
		operation := applyPatchOperationFromArgs(tc.Arguments)
		return ApplyPatchCall{
			Type:      "apply_patch_call",
			ID:        stringPtr(itemID),
			CallID:    callID,
			Status:    "completed",
			Operation: operation,
		}, true
	}
	if opts.CustomToolNames[tc.ToolName] || opts.CustomToolNames[toolName] {
		input := tc.RawArguments
		if input == "" {
			raw, _ := json.Marshal(tc.Arguments)
			input = string(raw)
		}
		return CustomToolCallItem{
			Type:   "custom_tool_call",
			ID:     itemID,
			CallID: tc.ID,
			Name:   toolName,
			Input:  input,
		}, true
	}
	return nil, false
}

func convertAssistantTextItem(part types.TextContent, opts ConvertOptions) interface{} {
	itemID, phase := openAITextMetadata(part, openAIProviderOptionsName(opts))
	if opts.HasConversation && itemID != "" {
		return nil
	}
	if opts.Store && itemID != "" {
		return map[string]interface{}{
			"type": "item_reference",
			"id":   itemID,
		}
	}
	return AssistantMessageItem{
		Type: "message",
		Role: "assistant",
		ID:   itemID,
		Phase: func() *string {
			if phase == "" {
				return nil
			}
			return &phase
		}(),
		Content: []AssistantMessageContent{{
			Type: "output_text",
			Text: part.Text,
		}},
	}
}

func convertReasoningItem(part types.ReasoningContent, opts ConvertOptions, reasoningItems map[string]map[string]interface{}) interface{} {
	itemID := openAIReasoningItemID(part, openAIProviderOptionsName(opts))
	if (opts.HasPreviousResponseID || opts.HasConversation) && itemID != "" {
		return nil
	}
	if opts.Store && itemID != "" {
		if _, ok := reasoningItems[itemID]; ok {
			return nil
		}
		reasoningItems[itemID] = map[string]interface{}{}
		return map[string]interface{}{
			"type": "item_reference",
			"id":   itemID,
		}
	}

	summaryParts := reasoningSummaryParts(part.Text)
	if itemID != "" {
		if existing, ok := reasoningItems[itemID]; ok {
			if len(summaryParts) > 0 {
				existing["summary"] = appendReasoningSummary(existing["summary"], summaryParts)
			}
			if part.EncryptedContent != "" {
				existing["encrypted_content"] = part.EncryptedContent
			}
			return nil
		}
		item := map[string]interface{}{
			"type":    "reasoning",
			"id":      itemID,
			"summary": summaryParts,
		}
		if part.EncryptedContent != "" {
			item["encrypted_content"] = part.EncryptedContent
		}
		reasoningItems[itemID] = item
		return item
	}
	if part.EncryptedContent == "" {
		return nil
	}

	item := map[string]interface{}{"type": "reasoning"}
	item["encrypted_content"] = part.EncryptedContent
	item["summary"] = summaryParts
	return item
}

func reasoningSummaryParts(text string) []map[string]interface{} {
	if text == "" {
		return []map[string]interface{}{}
	}
	return []map[string]interface{}{
		{
			"type": "summary_text",
			"text": text,
		},
	}
}

func appendReasoningSummary(existing interface{}, additional []map[string]interface{}) []map[string]interface{} {
	summary, _ := existing.([]map[string]interface{})
	return append(summary, additional...)
}

func filterReasoningItemsWithoutEncryptedContent(items []interface{}) []interface{} {
	filtered := items[:0]
	for _, item := range items {
		reasoning, ok := item.(map[string]interface{})
		if ok && reasoning["type"] == "reasoning" && reasoning["encrypted_content"] == nil {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func convertAssistantToolResultItem(part types.ToolResultContent, opts ConvertOptions) interface{} {
	if isExecutionDeniedOutput(part.Output) {
		return nil
	}
	if opts.HasConversation {
		return nil
	}
	toolName := normalizeOpenAIToolName(part.ToolName)
	if toolName == "tool_search" {
		itemID := firstNonEmpty(openAIToolResultItemID(part, openAIProviderOptionsName(opts)), part.ToolCallID)
		if opts.Store {
			return map[string]interface{}{
				"type": "item_reference",
				"id":   itemID,
			}
		}
		if part.Output != nil && part.Output.Type == types.ToolResultOutputJSON {
			if item := convertToolSearchOutput(part, "server"); item != nil {
				if output, ok := item.(ToolSearchOutputItem); ok {
					output.ID = itemID
					output.CallID = nil
					return output
				}
			}
		}
		return nil
	}
	if opts.HasShellTool && toolName == "shell" {
		if part.Output != nil && part.Output.Type == types.ToolResultOutputJSON {
			return convertShellOutput(part)
		}
		return nil
	}
	itemID := firstNonEmpty(openAIToolResultItemID(part, openAIProviderOptionsName(opts)), part.ToolCallID)
	if opts.Store && itemID != "" {
		return map[string]interface{}{
			"type": "item_reference",
			"id":   itemID,
		}
	}
	return nil
}

func convertAssistantCustomItem(part types.CustomContent, opts ConvertOptions) interface{} {
	if part.Kind != "openai-compaction" && part.Kind != "openai.compaction" {
		return nil
	}
	itemID, encryptedContent := openAICompactionMetadata(part, openAIProviderOptionsName(opts))
	if opts.HasConversation && itemID != "" {
		return nil
	}
	if opts.Store && itemID != "" {
		return map[string]interface{}{
			"type": "item_reference",
			"id":   itemID,
		}
	}
	if itemID == "" {
		return nil
	}
	item := map[string]interface{}{
		"type": "compaction",
		"id":   itemID,
	}
	if encryptedContent != "" {
		item["encrypted_content"] = encryptedContent
	}
	return item
}

func openAIReasoningItemID(part types.ReasoningContent, providerName string) string {
	if itemID := openAIItemID(part.ProviderOptions, providerName); itemID != "" {
		return itemID
	}
	if len(part.ProviderMetadata) == 0 {
		return ""
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(part.ProviderMetadata, &metadata); err != nil {
		return ""
	}
	return openAIItemID(metadata, providerName)
}

func openAIToolResultItemID(part types.ToolResultContent, providerName string) string {
	if itemID := openAIItemID(part.ProviderOptions, providerName); itemID != "" {
		return itemID
	}
	if len(part.ProviderMetadata) == 0 {
		return ""
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(part.ProviderMetadata, &metadata); err != nil {
		return ""
	}
	return openAIItemID(metadata, providerName)
}

func openAICompactionMetadata(part types.CustomContent, providerName string) (string, string) {
	itemID, encryptedContent := openAICompactionMetadataFromMap(part.ProviderOptions, providerName)
	if itemID != "" || encryptedContent != "" {
		return itemID, encryptedContent
	}
	if len(part.ProviderMetadata) == 0 {
		return "", ""
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(part.ProviderMetadata, &metadata); err != nil {
		return "", ""
	}
	itemID, encryptedContent = openAICompactionMetadataFromMap(metadata, providerName)
	if itemID != "" || encryptedContent != "" {
		return itemID, encryptedContent
	}
	itemID, _ = metadata["itemId"].(string)
	encryptedContent, _ = metadata["encryptedContent"].(string)
	return itemID, encryptedContent
}

func openAICompactionMetadataFromMap(metadata map[string]interface{}, providerName string) (string, string) {
	openaiMeta, ok := metadata[providerName].(map[string]interface{})
	if !ok {
		return "", ""
	}
	itemID, _ := openaiMeta["itemId"].(string)
	encryptedContent, _ := openaiMeta["encryptedContent"].(string)
	return itemID, encryptedContent
}

func openAITextMetadata(part types.TextContent, providerName string) (string, string) {
	itemID, phase := openAIItemIDAndPhase(part.ProviderOptions, providerName)
	if itemID != "" || phase != "" {
		return itemID, phase
	}
	if len(part.ProviderMetadata) == 0 {
		return "", ""
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(part.ProviderMetadata, &metadata); err != nil {
		return "", ""
	}
	return openAIItemIDAndPhase(metadata, providerName)
}

func openAIItemIDAndPhase(metadata map[string]interface{}, providerName string) (string, string) {
	openaiMeta, ok := metadata[providerName].(map[string]interface{})
	if !ok {
		return "", ""
	}
	itemID, _ := openaiMeta["itemId"].(string)
	phase, _ := openaiMeta["phase"].(string)
	return itemID, phase
}

func openAIItemID(metadata map[string]interface{}, providerName string) string {
	itemID, _ := openAIItemIDAndPhase(metadata, providerName)
	return itemID
}

func openAIProviderOptionsName(opts ConvertOptions) string {
	if opts.ProviderOptionsName != "" {
		return opts.ProviderOptionsName
	}
	return "openai"
}

func providerReferenceString(reference types.ProviderReference, providerName string) string {
	if len(reference) == 0 {
		return ""
	}
	if ref := reference[providerName]; ref != "" {
		return ref
	}
	return types.ProviderReferenceString(reference)
}

func toolCallContentProviderMetadata(part types.ToolCallContent) map[string]interface{} {
	metadata := map[string]interface{}{}
	for key, value := range part.ProviderOptions {
		metadata[key] = value
	}
	if len(part.ProviderMetadata) == 0 {
		return metadata
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(part.ProviderMetadata, &raw); err != nil {
		return metadata
	}
	for key, value := range raw {
		if _, ok := metadata[key]; !ok {
			metadata[key] = value
		}
	}
	return metadata
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// convertToolItems maps tool-role message content to function_call_output items.
func convertToolItems(msg types.Message, options ...ConvertOptions) []interface{} {
	var opts ConvertOptions
	if len(options) > 0 {
		opts = options[0]
	}
	items := make([]interface{}, 0, len(msg.Content))
	processedApprovals := map[string]bool{}
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.ToolApprovalResponseContent:
			if p.ApprovalID == "" || processedApprovals[p.ApprovalID] {
				continue
			}
			processedApprovals[p.ApprovalID] = true
			if opts.Store {
				items = append(items, map[string]interface{}{
					"type": "item_reference",
					"id":   p.ApprovalID,
				})
			}
			items = append(items, MCPApprovalResponse{
				Type:              "mcp_approval_response",
				ApprovalRequestID: p.ApprovalID,
				Approve:           p.Approved,
			})
		case *types.ToolApprovalResponseContent:
			if p == nil || p.ApprovalID == "" || processedApprovals[p.ApprovalID] {
				continue
			}
			processedApprovals[p.ApprovalID] = true
			if opts.Store {
				items = append(items, map[string]interface{}{
					"type": "item_reference",
					"id":   p.ApprovalID,
				})
			}
			items = append(items, MCPApprovalResponse{
				Type:              "mcp_approval_response",
				ApprovalRequestID: p.ApprovalID,
				Approve:           p.Approved,
			})
		case types.ToolResultContent:
			if shouldSkipApprovalDeniedOutput(p) {
				continue
			}
			if item := convertSpecialToolOutput(p, opts); item != nil {
				items = append(items, item)
				continue
			}
			if isOpenAICustomToolName(p.ToolName, opts.CustomToolNames) {
				items = append(items, CustomToolCallOutput{
					Type:   "custom_tool_call_output",
					CallID: p.ToolCallID,
					Output: toolResultOutputWithOptions(p, opts),
				})
				continue
			}
			items = append(items, FunctionCallOutputItem{
				Type:   "function_call_output",
				CallID: p.ToolCallID,
				Output: toolResultOutputWithOptions(p, opts),
			})
		case *types.ToolResultContent:
			if p != nil {
				if shouldSkipApprovalDeniedOutput(*p) {
					continue
				}
				if item := convertSpecialToolOutput(*p, opts); item != nil {
					items = append(items, item)
					continue
				}
				if isOpenAICustomToolName(p.ToolName, opts.CustomToolNames) {
					items = append(items, CustomToolCallOutput{
						Type:   "custom_tool_call_output",
						CallID: p.ToolCallID,
						Output: toolResultOutputWithOptions(*p, opts),
					})
					continue
				}
				items = append(items, FunctionCallOutputItem{
					Type:   "function_call_output",
					CallID: p.ToolCallID,
					Output: toolResultOutputWithOptions(*p, opts),
				})
			}
		}
	}
	return items
}

func convertSpecialToolOutput(part types.ToolResultContent, opts ConvertOptions) interface{} {
	if part.Output == nil || part.Output.Type != types.ToolResultOutputJSON {
		return nil
	}
	toolName := normalizeOpenAIToolName(part.ToolName)
	switch toolName {
	case "tool_search":
		return convertToolSearchOutput(part, "client")
	case "local_shell":
		if !opts.HasLocalShellTool {
			return nil
		}
		return convertLocalShellOutput(part)
	case "shell":
		if !opts.HasShellTool {
			return nil
		}
		return convertShellOutput(part)
	case "apply_patch":
		if !opts.HasApplyPatchTool {
			return nil
		}
		return convertApplyPatchOutput(part)
	default:
		return nil
	}
}

func convertToolSearchOutput(part types.ToolResultContent, execution string) interface{} {
	var parsed struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if !decodeToolOutputJSON(part.Output.Value, &parsed) {
		return nil
	}
	var callID *string
	if execution == "client" {
		callID = &part.ToolCallID
	}
	return ToolSearchOutputItem{
		Type:      "tool_search_output",
		Execution: execution,
		CallID:    callID,
		Status:    "completed",
		Tools:     parsed.Tools,
	}
}

func convertLocalShellOutput(part types.ToolResultContent) interface{} {
	var parsed struct {
		Output string `json:"output"`
	}
	if !decodeToolOutputJSON(part.Output.Value, &parsed) {
		return nil
	}
	return LocalShellCallOutput{
		Type:   "local_shell_call_output",
		CallID: part.ToolCallID,
		Output: parsed.Output,
	}
}

func convertShellOutput(part types.ToolResultContent) interface{} {
	var parsed struct {
		Output []struct {
			Stdout  string `json:"stdout"`
			Stderr  string `json:"stderr"`
			Outcome struct {
				Type          string `json:"type"`
				ExitCode      *int   `json:"exitCode"`
				ExitCodeSnake *int   `json:"exit_code"`
			} `json:"outcome"`
		} `json:"output"`
	}
	if !decodeToolOutputJSON(part.Output.Value, &parsed) {
		return nil
	}
	output := make([]ShellCallOutputEntry, 0, len(parsed.Output))
	for _, entry := range parsed.Output {
		exitCode := entry.Outcome.ExitCode
		if exitCode == nil {
			exitCode = entry.Outcome.ExitCodeSnake
		}
		output = append(output, ShellCallOutputEntry{
			Stdout: entry.Stdout,
			Stderr: entry.Stderr,
			Outcome: ShellOutcome{
				Type:     entry.Outcome.Type,
				ExitCode: exitCode,
			},
		})
	}
	return ShellCallOutput{
		Type:   "shell_call_output",
		CallID: part.ToolCallID,
		Output: output,
	}
}

func convertApplyPatchOutput(part types.ToolResultContent) interface{} {
	var parsed struct {
		Status string  `json:"status"`
		Output *string `json:"output"`
	}
	if !decodeToolOutputJSON(part.Output.Value, &parsed) {
		return nil
	}
	return ApplyPatchCallOutput{
		Type:   "apply_patch_call_output",
		CallID: part.ToolCallID,
		Status: parsed.Status,
		Output: parsed.Output,
	}
}

func normalizeOpenAIToolName(name string) string {
	return strings.TrimPrefix(name, "openai.")
}

func isOpenAICustomToolName(name string, customToolNames map[string]bool) bool {
	if len(customToolNames) == 0 {
		return false
	}
	return customToolNames[name] || customToolNames[normalizeOpenAIToolName(name)]
}

func localShellActionFromArgs(args map[string]interface{}) LocalShellAction {
	action, _ := args["action"].(map[string]interface{})
	return LocalShellAction{
		Type:             "exec",
		Command:          stringSliceArg(action, "command"),
		TimeoutMs:        intPtrArg(action, "timeoutMs"),
		User:             stringPtrArg(action, "user"),
		WorkingDirectory: stringPtrArg(action, "workingDirectory"),
		Env:              stringMapArg(action, "env"),
	}
}

func shellActionFromArgs(args map[string]interface{}) ShellCallAction {
	action, _ := args["action"].(map[string]interface{})
	return ShellCallAction{
		Commands:        stringSliceArg(action, "commands"),
		TimeoutMs:       intPtrArg(action, "timeoutMs"),
		MaxOutputLength: intPtrArg(action, "maxOutputLength"),
	}
}

func applyPatchOperationFromArgs(args map[string]interface{}) ApplyPatchOperation {
	operation, _ := args["operation"].(map[string]interface{})
	diff := stringPtrArg(operation, "diff")
	return ApplyPatchOperation{
		Type: stringArg(operation, "type"),
		Path: stringArg(operation, "path"),
		Diff: diff,
	}
}

func stringArg(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringPtrArg(values map[string]interface{}, key string) *string {
	return stringPtr(stringArg(values, key))
}

func intPtrArg(values map[string]interface{}, key string) *int {
	if values == nil {
		return nil
	}
	switch value := values[key].(type) {
	case int:
		return &value
	case int64:
		v := int(value)
		return &v
	case float64:
		v := int(value)
		return &v
	default:
		return nil
	}
}

func stringSliceArg(values map[string]interface{}, key string) []string {
	if values == nil {
		return nil
	}
	switch raw := values[key].(type) {
	case []string:
		return raw
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func stringMapArg(values map[string]interface{}, key string) map[string]string {
	if values == nil {
		return nil
	}
	switch raw := values[key].(type) {
	case map[string]string:
		return raw
	case map[string]interface{}:
		out := make(map[string]string, len(raw))
		for k, v := range raw {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

func decodeToolOutputJSON(value interface{}, target interface{}) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}

func isExecutionDeniedOutput(output *types.ToolResultOutput) bool {
	if output == nil {
		return false
	}
	if output.Type == types.ToolResultOutputExecutionDenied {
		return true
	}
	if output.Type != types.ToolResultOutputJSON {
		return false
	}
	value, ok := output.Value.(map[string]interface{})
	if !ok {
		return false
	}
	rawType, _ := value["type"].(string)
	return rawType == "execution-denied"
}

func shouldSkipApprovalDeniedOutput(part types.ToolResultContent) bool {
	if part.Output == nil || part.Output.Type != types.ToolResultOutputExecutionDenied {
		return false
	}
	openaiOptions, ok := part.ProviderOptions["openai"].(map[string]interface{})
	if !ok {
		return false
	}
	approvalID, _ := openaiOptions["approvalId"].(string)
	return approvalID != ""
}

// toolResultOutput converts a ToolResultContent to the Responses API output value.
// Returns a plain string for text/json outputs, or a []CustomToolCallOutputPart for
// content-array outputs (text, image-data, image-url, file-data, file-url).
func toolResultOutput(tr types.ToolResultContent) interface{} {
	return toolResultOutputWithOptions(tr, ConvertOptions{})
}

func toolResultOutputWithOptions(tr types.ToolResultContent, opts ConvertOptions) interface{} {
	providerName := openAIProviderOptionsName(opts)
	if tr.Output != nil {
		switch tr.Output.Type {
		case types.ToolResultOutputText:
			if s, ok := tr.Output.Value.(string); ok {
				return s
			}
		case types.ToolResultOutputJSON:
			if b, err := json.Marshal(tr.Output.Value); err == nil {
				return string(b)
			}
		case types.ToolResultOutputExecutionDenied:
			if tr.Output.Reason != "" {
				return tr.Output.Reason
			}
			return "Tool call execution denied."
		case types.ToolResultOutputContent:
			parts := make([]CustomToolCallOutputPart, 0, len(tr.Output.Content))
			for _, block := range tr.Output.Content {
				switch b := block.(type) {
				case types.TextContentBlock:
					parts = append(parts, CustomToolCallOutputPart{
						Type: "input_text",
						Text: b.Text,
					})
				case types.ImageContentBlock:
					imageURL := fmt.Sprintf("data:%s;base64,%s",
						b.MediaType, base64.StdEncoding.EncodeToString(b.Data))
					parts = append(parts, CustomToolCallOutputPart{
						Type:     "input_image",
						ImageURL: imageURL,
						Detail:   openAIResponsesImageDetail(b.ProviderOptions, providerName),
					})
				case types.FileContentBlock:
					mediaType := firstNonEmpty(b.MediaType, b.FileData.MediaType)
					fileDataType := b.FileData.Type
					if fileDataType == "" {
						switch {
						case b.URL != "":
							fileDataType = types.FileDataTypeURL
						case len(b.Data) > 0:
							fileDataType = types.FileDataTypeData
						case b.FileData.DataString != "" || len(b.FileData.Data) > 0:
							fileDataType = types.FileDataTypeData
						}
					}
					switch fileDataType {
					case types.FileDataTypeURL:
						url := firstNonEmpty(b.FileData.URL, b.URL)
						if url == "" {
							continue
						}
						if isImageMediaType(mediaType) {
							parts = append(parts, CustomToolCallOutputPart{
								Type:     "input_image",
								ImageURL: url,
								Detail:   openAIResponsesImageDetail(b.ProviderOptions, providerName),
							})
						} else {
							parts = append(parts, CustomToolCallOutputPart{
								Type:    "input_file",
								FileURL: url,
							})
						}
					case types.FileDataTypeData:
						data := fileContentBlockDataBase64(b)
						if data == "" {
							continue
						}
						fileData := fmt.Sprintf("data:%s;base64,%s", mediaType, data)
						if isImageMediaType(mediaType) {
							parts = append(parts, CustomToolCallOutputPart{
								Type:     "input_image",
								ImageURL: fileData,
								Detail:   openAIResponsesImageDetail(b.ProviderOptions, providerName),
							})
						} else {
							filename := b.Filename
							if filename == "" {
								filename = "data"
							}
							parts = append(parts, CustomToolCallOutputPart{
								Type:     "input_file",
								Filename: filename,
								FileData: fileData,
							})
						}
					}
				}
			}
			if len(parts) > 0 {
				return parts
			}
		}
	}
	if s, ok := tr.Result.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", tr.Result)
}

func fileContentBlockDataBase64(block types.FileContentBlock) string {
	if block.FileData.DataString != "" {
		return block.FileData.DataString
	}
	if len(block.FileData.Data) > 0 {
		return base64.StdEncoding.EncodeToString(block.FileData.Data)
	}
	if len(block.Data) > 0 {
		return base64.StdEncoding.EncodeToString(block.Data)
	}
	return ""
}

func openAIResponsesImageDetail(providerOptions map[string]interface{}, providerName ...string) string {
	if providerOptions == nil {
		return ""
	}
	name := "openai"
	if len(providerName) > 0 && providerName[0] != "" {
		name = providerName[0]
	}
	openaiOpts, ok := providerOptions[name].(map[string]interface{})
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
