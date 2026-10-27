package responses

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutil "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
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
	input, _, _ := ConvertPromptToInputWithOptions(prompt, systemMessageMode, ConvertOptions{PassThroughUnsupportedFiles: true})
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
	HasComputerTool             bool
	FileIDPrefixes              []string
	ProviderOptionsName         string
	// IsReasoningModel, when true, means stored assistant text is only
	// replaced by an item_reference when it follows a reasoning part of the
	// same message. Stored messages of reasoning models are bound to the
	// reasoning item that produced them; when the reasoning was removed
	// (e.g. by pruning), referencing the message fails, so it is sent with
	// its content instead (TS #23123).
	IsReasoningModel bool
	// WebSearchToolName is the request's name for the web search tool
	// (openai.web_search or openai.web_search_preview), used to recognize
	// provider-executed web search results in assistant history. Defaults to
	// matching "web_search" / "web_search_preview".
	WebSearchToolName string
	// ExplicitMessageItemType, when true, adds `"type":"message"` to easy-input
	// assistant/system messages (required by Azure Foundry projects). See TS
	// convert-to-openai-responses-input.ts `explicitMessageItemType`.
	ExplicitMessageItemType bool
	// ToolSearchToolName is the exact SDK tool name whose ProviderID is
	// "openai.tool_search" for this request, if any. Only a tool call/result
	// with this exact name is treated as the tool_search feature; a regular
	// function tool that happens to be named "tool_search" is not.
	ToolSearchToolName string

	// ProviderToolNames maps the SDK tool name a caller declared for an OpenAI
	// provider-defined tool (e.g. "program" for openai.programmatic_tool_calling)
	// to the bare provider tool name ("programmatic_tool_calling"), like the
	// TS toolNameMapping.toProviderToolName. Names without an entry fall back
	// to trimming an "openai." prefix.
	ProviderToolNames map[string]string
	// IncludePromptCacheBreakpoint forwards providerOptions.<name>.
	// promptCacheBreakpoint as prompt_cache_breakpoint on system messages,
	// user parts and tool outputs (TS convertToOpenAIResponsesInput). TS's
	// separate xAI Responses converter has no such option, so it stays off
	// for callers other than the OpenAI/Azure Responses models.
	IncludePromptCacheBreakpoint bool

	// OutputSchemaToolNames holds the names of function tools that declared
	// providerOptions.openai.outputSchema. OpenAI parses their
	// function_call_output.output as JSON, so text-like results must be
	// JSON-encoded (JSON.stringify'd) before being sent.
	OutputSchemaToolNames map[string]bool

	// ConfigurationUpdateUnsupportedReason, when non-empty, is the reason a
	// mid-conversation reasoningEffortUpdate configuration_update item
	// cannot be emitted for this request (e.g. the model doesn't support
	// GPT-6+ configuration updates, or reasoningMode/contextManagement/
	// truncation are incompatible with it). Computed once per request by the
	// caller (mirrors TS getConfigurationUpdateUnsupportedReason) and
	// applied uniformly to every message-level reasoningEffortUpdate found
	// while converting the prompt, matching TS
	// convert-to-openai-responses-input.ts's `configurationUpdateUnsupportedReason`
	// parameter.
	ConfigurationUpdateUnsupportedReason string

	// programmaticCallerIDs holds tool-call-ids whose caller was a
	// programmatic-tool-calling "program", collected from the prompt's
	// assistant messages by ConvertPromptToInputWithOptions before
	// conversion. Used to reject execution-denied results for those calls
	// when the result itself doesn't carry its own caller metadata (row
	// e105b2b). Do not set directly.
	programmaticCallerIDs map[string]bool

	// parallelToolResultGroups, emittedParallelToolCalls and
	// emittedParallelToolResults support regrouping an expanded "parallel"
	// tool call wrapper's child results back into a single
	// function_call/function_call_output pair on replay, when Responses API
	// server-side state (conversation or previousResponseId) requires the
	// server's original wrapper call_id to be preserved (row 6be0f51).
	// Populated by ConvertPromptToInputWithOptions; do not set directly.
	parallelToolResultGroups   map[string]parallelToolResultGroup
	emittedParallelToolCalls   map[string]bool
	emittedParallelToolResults map[string]bool

	// processedApprovalIDs de-duplicates tool-approval responses across every
	// tool message of the prompt (TS processedApprovalIds is shared by the
	// whole conversion, not reset per message).
	processedApprovalIDs map[string]bool

	// toolOutputWarnings collects the "unsupported tool content part type"
	// warnings TS pushes while converting tool-result content outputs.
	// Populated by ConvertPromptToInputWithOptions; do not set directly.
	toolOutputWarnings *[]types.Warning

	// customToolOutput is set while converting a custom_tool_call_output,
	// whose TS converter has its own (narrower) content handling: provider
	// references are unsupported and warnings say "custom tool content part".
	customToolOutput bool
}

// warnToolOutput records a TS `{type: 'other', message}` warning raised while
// converting a tool-result content output.
func (o ConvertOptions) warnToolOutput(message string) {
	if o.toolOutputWarnings != nil {
		if o.customToolOutput {
			message = strings.Replace(message, "unsupported tool content part", "unsupported custom tool content part", 1)
		}
		*o.toolOutputWarnings = append(*o.toolOutputWarnings, types.Warning{Type: "other", Message: message})
	}
}

// parallelToolResultGroup is a complete set of child results for one
// expanded "parallel" tool call wrapper, ordered by their original index.
type parallelToolResultGroup struct {
	metadata ParallelToolCallMetadata
	results  []types.ToolResultContent
}

// toolSearchName returns the configured tool_search tool name, defaulting to
// the literal "tool_search" for backward compatibility when no tool in the
// request declares ProviderID "openai.tool_search".
func (o ConvertOptions) toolSearchName() string {
	if o.ToolSearchToolName != "" {
		return o.ToolSearchToolName
	}
	// The Go tool_search factory (openaitool.ToolSearch) always sets both Name
	// and ProviderID to "openai.tool_search" (Go does not support renaming
	// this tool), so that is the correct default when no request-specific
	// name has been resolved (e.g. direct ConvertOptions calls in tests).
	return "openai.tool_search"
}

// ConvertPromptToInputWithOptions converts a prompt to Responses API input and
// validates file media types according to OpenAI Responses defaults.
func ConvertPromptToInputWithOptions(prompt types.Prompt, systemMessageMode string, opts ConvertOptions) ([]interface{}, []types.Warning, error) {
	input := make([]interface{}, 0, len(prompt.Messages)+1)
	var warnings []types.Warning
	if err := validateToolOutputFileReferences(prompt, opts); err != nil {
		return nil, nil, err
	}
	opts.programmaticCallerIDs = collectProgrammaticCallerIDs(prompt, openAIProviderOptionsName(opts))
	if opts.HasConversation || opts.HasPreviousResponseID {
		opts.parallelToolResultGroups = collectCompleteParallelToolResultGroups(prompt, openAIProviderOptionsName(opts))
	} else {
		opts.parallelToolResultGroups = map[string]parallelToolResultGroup{}
	}
	opts.emittedParallelToolCalls = map[string]bool{}
	opts.emittedParallelToolResults = map[string]bool{}
	opts.processedApprovalIDs = map[string]bool{}
	var toolOutputWarnings []types.Warning
	opts.toolOutputWarnings = &toolOutputWarnings

	// Prepend system message when present and not suppressed.
	if prompt.System != "" && systemMessageMode != "remove" {
		sysMsg := SystemMessage{
			Role:    systemMessageMode,
			Content: prompt.System,
		}
		if opts.ExplicitMessageItemType {
			sysMsg.Type = "message"
		}
		input = append(input, sysMsg)
	}
	// TS treats the system prompt as the first system message, so "remove"
	// mode warns for it as well.
	if prompt.System != "" && systemMessageMode == "remove" {
		warnings = append(warnings, types.Warning{Type: "other", Message: "system messages are removed for this model"})
	}

	for _, msg := range prompt.Messages {
		switch msg.Role {
		case types.RoleSystem:
			item, itemWarning, err := convertSystemMidConversationMessage(msg, systemMessageMode, opts)
			if err != nil {
				return nil, warnings, err
			}
			if item != nil {
				input = append(input, item)
			}
			if itemWarning != nil {
				warnings = append(warnings, *itemWarning)
			}
		case types.RoleUser:
			userMessage, err := convertUserMessage(msg, opts)
			if err != nil {
				return nil, warnings, err
			}
			input = append(input, userMessage)
		case types.RoleAssistant:
			items, itemWarnings, err := convertAssistantItems(msg, opts)
			if err != nil {
				return nil, warnings, err
			}
			input = append(input, items...)
			warnings = append(warnings, itemWarnings...)
		case types.RoleTool:
			items, err := convertToolItems(msg, opts)
			if err != nil {
				return nil, warnings, err
			}
			input = append(input, items...)
			warnings = append(warnings, toolOutputWarnings...)
			toolOutputWarnings = toolOutputWarnings[:0]
		}
	}

	// TS filters the whole input once at the end (a single warning), not per
	// assistant message.
	if !opts.Store {
		filtered, filterWarnings := filterReasoningItemsWithoutEncryptedContent(input)
		input = filtered
		warnings = append(warnings, filterWarnings...)
	}

	return input, warnings, nil
}

// convertSystemMidConversationMessage converts a system-role Message found in
// prompt.Messages (as opposed to the hoisted prompt.System) to a Responses
// API input item. Mirrors TS convert-to-openai-responses-input.ts's
// `case 'system'`: a message whose providerOptions[provider].
// reasoningEffortUpdate is set becomes a positioned `configuration_update`
// item independent of systemMessageMode's text handling; a message with
// empty reasoningEffortUpdate is handled as an ordinary system/developer
// message (or dropped with a warning when systemMessageMode is "remove").
func convertSystemMidConversationMessage(msg types.Message, systemMessageMode string, opts ConvertOptions) (interface{}, *types.Warning, error) {
	providerName := openAIProviderOptionsName(opts)
	sysOpts := systemMessageProviderOptions(msg, providerName)
	effort, _ := sysOpts["reasoningEffortUpdate"].(string)
	content := systemMessageText(msg)
	// zod `.optional()` enum: a present key must hold a valid value (an empty
	// string, a number or an explicit null is rejected, not ignored).
	_, effortPresent := sysOpts["reasoningEffortUpdate"]

	if effortPresent {
		// TS parses this field against a fixed schema enum
		// (z.enum(['none','low','medium','high','xhigh','max']), row
		// 94d5d6d3e6) before any model-specific handling, so an
		// out-of-enum value like "minimal" is rejected the same way on
		// every model, including ones that support "none". Go has no
		// schema layer, so validate manually here, first.
		if !isValidReasoningEffortUpdateValue(effort) {
			return nil, nil, &providererrors.InvalidArgumentError{
				Field:   "providerOptions." + providerName + ".reasoningEffortUpdate",
				Message: fmt.Sprintf("must be one of %s", strings.Join(ValidReasoningEffortUpdateValues, ", ")),
			}
		}

		var unsupportedReason string
		if content != "" {
			unsupportedReason = "Message-level reasoningEffortUpdate requires empty system message content."
		} else {
			unsupportedReason = opts.ConfigurationUpdateUnsupportedReason
		}
		if unsupportedReason != "" {
			return nil, nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "Message-level reasoningEffortUpdate",
				Message:       unsupportedReason,
			}
		}

		// The control is independent of systemMessageMode's text handling.
		return map[string]interface{}{
			"type":      "configuration_update",
			"reasoning": map[string]interface{}{"effort": effort},
		}, nil, nil
	}

	switch systemMessageMode {
	case "system", "developer":
		if bp := opts.cacheBreakpoint(msg.ProviderOptions, providerName); bp != nil {
			item := map[string]interface{}{
				"role": systemMessageMode,
				"content": []interface{}{
					UserTextPart{Type: "input_text", Text: content, PromptCacheBreakpoint: bp},
				},
			}
			if opts.ExplicitMessageItemType {
				item["type"] = "message"
			}
			return item, nil, nil
		}
		sysMsg := SystemMessage{Role: systemMessageMode, Content: content}
		if opts.ExplicitMessageItemType {
			sysMsg.Type = "message"
		}
		return sysMsg, nil, nil
	case "remove":
		return nil, &types.Warning{
			Type:    "other",
			Message: "system messages are removed for this model",
		}, nil
	default:
		return nil, nil, fmt.Errorf("openai.responses: unsupported system message mode: %s", systemMessageMode)
	}
}

// systemMessageProviderOptions reads a system message's provider-specific
// options, falling back from an Azure-style provider name (e.g. "azure") to
// "openai" only when no entry is present at all under the request's own
// provider name -- an explicit (even empty) entry under the request's
// provider name is used as-is and does not fall back, matching TS
// parseProviderOptions semantics.
func systemMessageProviderOptions(msg types.Message, providerName string) map[string]interface{} {
	if msg.ProviderOptions == nil {
		return nil
	}
	if v, ok := msg.ProviderOptions[providerName].(map[string]interface{}); ok {
		return v
	}
	if providerName != "openai" {
		if v, ok := msg.ProviderOptions["openai"].(map[string]interface{}); ok {
			return v
		}
	}
	return nil
}

// systemMessageText extracts the plain text content of a system message.
func systemMessageText(msg types.Message) string {
	var b strings.Builder
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			b.WriteString(p.Text)
		case *types.TextContent:
			if p != nil {
				b.WriteString(p.Text)
			}
		}
	}
	return b.String()
}

// convertUserMessage maps a user-role Message to a UserMessage.
// Responses uses a typed content array for user messages, matching the
// TypeScript SDK converter even for single text messages.
func convertUserMessage(msg types.Message, opts ConvertOptions) (UserMessage, error) {
	providerName := openAIProviderOptionsName(opts)
	parts := make([]interface{}, 0, len(msg.Content))
	// The per-part prompt cache breakpoint is applied to the wire part the
	// iteration appended (a part may also be skipped via continue).
	var pendingBP interface{}
	pendingIdx := 0
	applyPendingBP := func() {
		if pendingBP != nil && len(parts) > pendingIdx {
			parts[pendingIdx] = withPromptCacheBreakpoint(parts[pendingIdx], pendingBP)
		}
		pendingBP = nil
	}
	for index, part := range msg.Content {
		applyPendingBP()
		pendingIdx = len(parts)
		switch p := part.(type) {
		case types.TextContent:
			pendingBP = opts.cacheBreakpoint(p.ProviderOptions, providerName)
		case types.ImageContent:
			pendingBP = opts.cacheBreakpoint(p.ProviderOptions, providerName)
		case types.FileContent:
			pendingBP = opts.cacheBreakpoint(p.ProviderOptions, providerName)
		}
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
					// TS returns a URL file part before any media type check.
					parts = append(parts, UserFilePart{Type: "input_file", FileURL: fileURL})
				}
			case types.FileDataTypeReference:
				reference, refErr := resolveProviderReferenceStrict(p.FileData.Reference, providerName)
				if refErr != nil {
					return UserMessage{}, refErr
				}
				reference = firstNonEmpty(reference, p.Reference)
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
				return UserMessage{}, &providererrors.UnsupportedFunctionalityError{Functionality: "text file parts"}
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
						// TS resolves the full media type and applies the
						// passThroughUnsupportedFiles check before the file-id
						// branch of a non-image data part.
						resolvedMediaType, resolveErr := resolveInlineBase64MediaType(mediaType, p.FileData.DataString)
						if resolveErr != nil {
							return UserMessage{}, resolveErr
						}
						if err := validateResponsesFileMediaType(resolvedMediaType, opts.PassThroughUnsupportedFiles, providerName); err != nil {
							return UserMessage{}, err
						}
						parts = append(parts, map[string]interface{}{"type": "input_file", "file_id": fileID})
					}
					continue
				}
				data := openAIFileDataBase64(p)
				if data == "" {
					continue
				}
				resolvedMediaType, resolveErr := resolveInlineBase64MediaType(mediaType, data)
				if resolveErr != nil {
					return UserMessage{}, resolveErr
				}
				mediaType = resolvedMediaType
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

	applyPendingBP()
	userMsg := UserMessage{Role: "user", Content: parts}
	if opts.ExplicitMessageItemType {
		userMsg.Type = "message"
	}
	return userMsg, nil
}

func openAIFileMediaType(part types.FileContent) string {
	return firstNonEmpty(part.MediaType, part.MimeType, part.FileData.MediaType)
}

// resolveInlineBase64MediaType is TS resolveFullMediaType for a file part with
// inline bytes (given base64-encoded): a full type/subtype is kept, a bare or
// wildcard type (image, image/*) is sniffed from the bytes, and bytes that
// cannot be identified are an UnsupportedFunctionalityError. An empty media
// type keeps the legacy pass-through.
func resolveInlineBase64MediaType(mediaType, base64Data string) (string, error) {
	if mediaType == "" {
		return mediaType, nil
	}
	if full, err := promptutil.ResolveFullMediaType(mediaType, nil, true); err == nil {
		return full, nil
	}
	raw, err := types.DecodeFileDataString(base64Data)
	if err != nil {
		return mediaType, nil
	}
	return promptutil.ResolveFullMediaType(mediaType, raw, true)
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

func validateResponsesFileMediaType(mediaType string, passThroughUnsupportedFiles bool, _ string) error {
	if passThroughUnsupportedFiles || mediaType == "" || mediaType == "application/pdf" {
		return nil
	}
	// TS: throw new UnsupportedFunctionalityError({ functionality: `file part media type ${fullMediaType}` })
	return &providererrors.UnsupportedFunctionalityError{Functionality: "file part media type " + mediaType}
}

// convertAssistantItems maps assistant-role content parts and ToolCalls to
// Responses API input items.
func convertAssistantItems(msg types.Message, opts ConvertOptions) ([]interface{}, []types.Warning, error) {
	items := make([]interface{}, 0, 1+len(msg.ToolCalls))
	var warnings []types.Warning
	providerName := openAIProviderOptionsName(opts)

	reasoningItems := map[string]map[string]interface{}{}
	toolCallContentIDs := map[string]bool{}
	// A message's text parts all share the stored message id; reference each
	// stored message only once (TS #22214).
	emittedTextItemIDs := map[string]bool{}
	hasPrecedingReasoning := false
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			if item := convertAssistantTextItem(p, opts, hasPrecedingReasoning, emittedTextItemIDs); item != nil {
				items = append(items, item)
			}
		case types.ReasoningContent:
			hasPrecedingReasoning = true
			item, w := convertReasoningItem(p, opts, reasoningItems)
			if item != nil {
				items = append(items, item)
			}
			warnings = append(warnings, w...)
		case types.ToolCallContent:
			toolCallContentIDs[p.ToolCallID] = true
			item, err := convertAssistantToolCallContentItem(p, opts)
			if err != nil {
				return nil, nil, err
			}
			if item != nil {
				items = append(items, item)
			}
		case *types.ToolCallContent:
			if p != nil {
				toolCallContentIDs[p.ToolCallID] = true
				item, err := convertAssistantToolCallContentItem(*p, opts)
				if err != nil {
					return nil, nil, err
				}
				if item != nil {
					items = append(items, item)
				}
			}
		case types.ToolResultContent:
			item, err := convertAssistantToolResultItem(p, opts)
			if err != nil {
				return nil, nil, err
			}
			if item != nil {
				items = append(items, item)
			} else if w := droppedToolResultWarning(p, opts); w != nil {
				warnings = append(warnings, *w)
			}
		case *types.ToolResultContent:
			if p != nil {
				item, err := convertAssistantToolResultItem(*p, opts)
				if err != nil {
					return nil, nil, err
				}
				if item != nil {
					items = append(items, item)
				} else if w := droppedToolResultWarning(*p, opts); w != nil {
					warnings = append(warnings, *w)
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
	// Each ToolCall on the message becomes a function_call item.
	for _, tc := range msg.ToolCalls {
		if toolCallContentIDs[tc.ID] {
			continue
		}
		itemID := openAIItemID(tc.ProviderMetadata, providerName)
		if opts.HasConversation && itemID != "" {
			continue
		}
		item, handled, err := convertAssistantToolCallItem(tc, itemID, opts)
		if err != nil {
			return nil, nil, err
		}
		if handled {
			if item != nil {
				items = append(items, item)
			}
			continue
		}
		// Plain client-executed function calls must always be resent in full,
		// even when chaining with previousResponseId: the matching
		// function_call_output can only reference the call by call_id, which
		// the API cannot reconcile with an item id or item_reference.
		args := tc.Arguments
		if args == nil {
			args = map[string]interface{}{}
		}
		argsJSON := []byte(providerutils.ArgumentsJSONText(args, tc.RawArguments))
		namespace := ""
		var async *bool
		if openaiMeta, ok := tc.ProviderMetadata[providerName].(map[string]interface{}); ok {
			if rawNS, ok := openaiMeta["namespace"].(string); ok {
				namespace = rawNS
			}
			if rawAsync, ok := openaiMeta["async"].(bool); ok {
				async = &rawAsync
			}
		}
		items = append(items, FunctionCallItem{
			Type:      "function_call",
			CallID:    tc.ID,
			Name:      tc.ToolName,
			Namespace: namespace,
			Async:     async,
			Caller:    toolCallCallerFromMetadata(tc.ProviderMetadata, providerName),
			Arguments: string(argsJSON),
		})
	}

	return items, warnings, nil
}

func convertAssistantToolCallContentItem(part types.ToolCallContent, opts ConvertOptions) (interface{}, error) {
	providerName := openAIProviderOptionsName(opts)
	metadata := toolCallContentProviderMetadata(part)
	itemID := openAIItemID(metadata, providerName)
	if opts.HasConversation && itemID != "" {
		return nil, nil
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
	if item, handled, err := convertAssistantToolCallItem(tc, itemID, opts); handled || err != nil {
		return item, err
	}
	// Plain client-executed function calls must always be resent in full (see
	// the comment in convertAssistantItems).
	namespace := ""
	var async *bool
	if openaiMeta, ok := tc.ProviderMetadata[providerName].(map[string]interface{}); ok {
		if rawNS, ok := openaiMeta["namespace"].(string); ok {
			namespace = rawNS
		}
		if rawAsync, ok := openaiMeta["async"].(bool); ok {
			async = &rawAsync
		}
	}
	return FunctionCallItem{
		Type:      "function_call",
		CallID:    tc.ID,
		Name:      tc.ToolName,
		Namespace: namespace,
		Async:     async,
		Caller:    toolCallCallerFromMetadata(tc.ProviderMetadata, providerName),
		Arguments: genericArguments,
	}, nil
}

// toolCallCallerFromMetadata extracts the caller (direct vs. a
// programmatic-tool-calling "program") from a decoded tool-call's
// ProviderMetadata, for forwarding onto a reconstructed plain function_call
// input item (row 1f6dd3a). Mirrors TS's caller.type === 'program' ?
// {type:'program', caller_id} : caller, applied in reverse (SDK -> wire).
func toolCallCallerFromMetadata(metadata map[string]interface{}, providerName string) *ToolCaller {
	openaiMeta, ok := metadata[providerName].(map[string]interface{})
	if !ok {
		return nil
	}
	caller, ok := openaiMeta["caller"].(map[string]interface{})
	if !ok {
		return nil
	}
	callerType, _ := caller["type"].(string)
	if callerType == "" {
		return nil
	}
	if callerType == "program" {
		callerID, _ := caller["callerId"].(string)
		return &ToolCaller{Type: "program", CallerID: callerID}
	}
	return &ToolCaller{Type: callerType}
}

// toolResultCaller extracts the caller from a decoded tool-result's
// ProviderOptions (checked first, matching TS's part.providerOptions
// lookup) or ProviderMetadata (checked second), for the
// execution-denied-for-programmatic-calls rejection below (row e105b2b).
func toolResultCaller(part types.ToolResultContent, providerName string) *ToolCaller {
	if openaiOptions, ok := part.ProviderOptions[providerName].(map[string]interface{}); ok {
		if caller, ok := openaiOptions["caller"].(map[string]interface{}); ok {
			callerType, _ := caller["type"].(string)
			if callerType == "program" {
				callerID, _ := caller["callerId"].(string)
				return &ToolCaller{Type: "program", CallerID: callerID}
			}
			if callerType != "" {
				return &ToolCaller{Type: callerType}
			}
		}
	}
	if len(part.ProviderMetadata) == 0 {
		return nil
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(part.ProviderMetadata, &metadata); err != nil {
		return nil
	}
	return toolCallCallerFromMetadata(metadata, providerName)
}

func toolCallContentArguments(part types.ToolCallContent) (map[string]interface{}, string) {
	if part.Arguments != nil {
		return part.Arguments, providerutils.ArgumentsJSONText(part.Arguments, part.Input)
	}
	if part.Input == "" {
		return map[string]interface{}{}, "{}"
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(part.Input), &parsed); err != nil {
		argsJSON, _ := jsonstr.Marshal(part.Input)
		return map[string]interface{}{}, string(argsJSON)
	}
	if args, ok := parsed.(map[string]interface{}); ok {
		return args, providerutils.ArgumentsJSONText(args, part.Input)
	}
	argsJSON, _ := jsonstr.Marshal(parsed)
	return map[string]interface{}{}, string(argsJSON)
}

func convertAssistantToolCallItem(tc types.ToolCall, itemID string, opts ConvertOptions) (interface{}, bool, error) {
	// Validated payloads (shell, computer, tool_search, ...) are read from the
	// parsed input; recover it from the wire text when only that was set.
	if tc.Arguments == nil && strings.TrimSpace(tc.RawArguments) != "" {
		var parsed map[string]interface{}
		if json.Unmarshal([]byte(tc.RawArguments), &parsed) == nil {
			tc.Arguments = parsed
		}
	}
	// Row 6be0f51: a tool call expanded from an internal "parallel" wrapper
	// replays as the ORIGINAL wrapper's single function_call (only once, and
	// only when the server doesn't already remember it via conversation
	// state), not as its own individual function_call.
	if metadata, ok := GetParallelToolCallMetadata(tc.ProviderMetadata, openAIProviderOptionsName(opts)); ok {
		if group, exists := opts.parallelToolResultGroups[metadata.ToolCallID]; exists && sameParallelToolCall(group.metadata, metadata) {
			if opts.emittedParallelToolCalls[group.metadata.ToolCallID] {
				return nil, true, nil
			}
			opts.emittedParallelToolCalls[group.metadata.ToolCallID] = true
			if opts.HasConversation {
				// Conversations already contain the original wrapper item.
				return nil, true, nil
			}
			return FunctionCallItem{
				Type:      "function_call",
				CallID:    group.metadata.ToolCallID,
				Name:      group.metadata.ToolName,
				Arguments: group.metadata.Input,
			}, true, nil
		}
	}
	toolName := opts.providerToolName(tc.ToolName)
	if tc.ToolName == opts.toolSearchName() {
		if opts.Store && itemID != "" {
			return map[string]interface{}{"type": "item_reference", "id": itemID}, true, nil
		}
		if err := validateToolPayload(toolSearchInputNode, tc.Arguments); err != nil {
			return nil, true, err
		}
		arguments := tc.Arguments
		var callID *string
		execution := "server"
		// TS: `parsedInput.call_id != null ? 'client' : 'server'` -- an empty
		// string call_id is still a client call.
		if rawCallID, ok := arguments["call_id"].(string); ok {
			callID = &rawCallID
			execution = "client"
		}
		// TS: `arguments: parsedInput.arguments` (omitted when absent).
		var rawArguments json.RawMessage
		if nested, ok := arguments["arguments"]; ok {
			encoded, _ := jsonstr.Marshal(nested)
			rawArguments = encoded
		}
		// TS: `id: id ?? part.toolCallId` -- unlike local_shell_call/
		// shell_call/apply_patch_call/computer_call (which use a bare `id!`
		// non-null assertion with no fallback), tool_search_call falls back
		// to the tool call's own id when no persisted itemId exists.
		id := itemID
		if id == "" {
			id = tc.ID
		}
		return ToolSearchCallItem{
			Type:      "tool_search_call",
			ID:        id,
			Status:    "completed",
			Execution: execution,
			CallID:    callID,
			Arguments: rawArguments,
		}, true, nil
	}
	if opts.providerToolName(tc.ToolName) == "programmatic_tool_calling" {
		// Row 1f6dd3a: checked before the generic ProviderExecuted branch
		// below, mirroring TS placing its 'programmatic_tool_calling' check
		// ahead of the generic providerExecuted branch.
		if opts.Store && itemID != "" {
			return map[string]interface{}{"type": "item_reference", "id": itemID}, true, nil
		}
		id := itemID
		if id == "" {
			id = tc.ID
		}
		if err := validateToolPayload(programInputNode, tc.Arguments); err != nil {
			return nil, true, err
		}
		code, _ := tc.Arguments["code"].(string)
		fingerprint, _ := tc.Arguments["fingerprint"].(string)
		return ProgramItem{
			Type:        "program",
			ID:          id,
			CallID:      tc.ID,
			Code:        code,
			Fingerprint: fingerprint,
		}, true, nil
	}
	if tc.ProviderExecuted {
		if opts.Store && itemID != "" {
			return map[string]interface{}{"type": "item_reference", "id": itemID}, true, nil
		}
		// Without response storage, shell calls must be reconstructed together
		// with their matching shell_call_output; every other provider-executed
		// call type is simply omitted when not stored.
		if opts.Store || !opts.HasShellTool || toolName != "shell" {
			return nil, true, nil
		}
		// fall through to the shell reconstruction branch below.
	}
	if opts.Store && itemID != "" && isProviderDefinedResponsesTool(toolName, tc.ToolName, opts) {
		if opts.HasPreviousResponseID {
			return nil, true, nil
		}
		return map[string]interface{}{"type": "item_reference", "id": itemID}, true, nil
	}
	if opts.HasLocalShellTool && toolName == "local_shell" {
		action, err := localShellActionFromArgs(tc.Arguments)
		if err != nil {
			return nil, true, err
		}
		return LocalShellCall{
			Type:   "local_shell_call",
			ID:     itemID,
			CallID: tc.ID,
			Action: action,
		}, true, nil
	}
	if opts.HasShellTool && toolName == "shell" {
		action, err := shellActionFromArgs(tc.Arguments)
		if err != nil {
			return nil, true, err
		}
		return ShellCall{
			Type:   "shell_call",
			ID:     itemID,
			CallID: tc.ID,
			Status: "completed",
			Action: action,
		}, true, nil
	}
	if opts.HasApplyPatchTool && toolName == "apply_patch" {
		callID, operation, err := applyPatchFromArgs(tc.Arguments)
		if err != nil {
			return nil, true, err
		}
		return ApplyPatchCall{
			Type:      "apply_patch_call",
			ID:        stringPtr(itemID),
			CallID:    callID,
			Status:    "completed",
			Operation: operation,
		}, true, nil
	}
	if opts.HasComputerTool && toolName == "computer" {
		callID := tc.ID
		status, actions, checks, err := computerCallFromArgs(tc.Arguments)
		if err != nil {
			return nil, true, err
		}
		return ComputerCall{
			Type:                "computer_call",
			ID:                  stringPtr(itemID),
			CallID:              &callID,
			Status:              status,
			Actions:             actions,
			PendingSafetyChecks: checks,
		}, true, nil
	}
	if opts.CustomToolNames[tc.ToolName] || opts.CustomToolNames[toolName] {
		input := tc.RawArguments
		if input == "" {
			raw, _ := jsonstr.Marshal(tc.Arguments)
			input = string(raw)
		}
		var async *bool
		if openaiMeta, ok := tc.ProviderMetadata[openAIProviderOptionsName(opts)].(map[string]interface{}); ok {
			if rawAsync, ok := openaiMeta["async"].(bool); ok {
				async = &rawAsync
			}
		}
		return CustomToolCallItem{
			Type:   "custom_tool_call",
			ID:     itemID,
			CallID: tc.ID,
			Name:   toolName,
			Async:  async,
			Input:  input,
		}, true, nil
	}
	return nil, false, nil
}

func isProviderDefinedResponsesTool(normalizedToolName, originalToolName string, opts ConvertOptions) bool {
	return (opts.HasLocalShellTool && normalizedToolName == "local_shell") ||
		(opts.HasShellTool && normalizedToolName == "shell") ||
		(opts.HasApplyPatchTool && normalizedToolName == "apply_patch") ||
		(opts.HasComputerTool && normalizedToolName == "computer") ||
		opts.CustomToolNames[originalToolName] ||
		opts.CustomToolNames[normalizedToolName]
}

// convertAssistantTextItem builds an "easy input message" for assistant text.
// Per TS convert-to-openai-responses-input.ts, this never includes an `id`
// field (an id present with store=false is simply dropped, since the text is
// being resent as fresh content); `type` is only added when
// opts.ExplicitMessageItemType is set (Azure Foundry projects).
func convertAssistantTextItem(part types.TextContent, opts ConvertOptions, hasPrecedingReasoning bool, emittedTextItemIDs map[string]bool) interface{} {
	itemID, phase := openAITextMetadata(part, openAIProviderOptionsName(opts))
	if opts.HasConversation && itemID != "" {
		return nil
	}
	if opts.Store && itemID != "" && (!opts.IsReasoningModel || hasPrecedingReasoning) {
		if emittedTextItemIDs[itemID] {
			return nil
		}
		emittedTextItemIDs[itemID] = true
		return map[string]interface{}{
			"type": "item_reference",
			"id":   itemID,
		}
	}
	item := map[string]interface{}{
		"role":    "assistant",
		"content": part.Text,
	}
	if opts.ExplicitMessageItemType {
		item["type"] = "message"
	}
	if phase != "" {
		item["phase"] = phase
	}
	return item
}

func convertReasoningItem(part types.ReasoningContent, opts ConvertOptions, reasoningItems map[string]map[string]interface{}) (interface{}, []types.Warning) {
	itemID := openAIReasoningItemID(part, openAIProviderOptionsName(opts))
	if (opts.HasPreviousResponseID || opts.HasConversation) && itemID != "" {
		return nil, nil
	}
	if opts.Store && itemID != "" {
		if _, ok := reasoningItems[itemID]; ok {
			return nil, nil
		}
		reasoningItems[itemID] = map[string]interface{}{}
		return map[string]interface{}{
			"type": "item_reference",
			"id":   itemID,
		}, nil
	}

	summaryParts := reasoningSummaryParts(part.Text)
	if itemID != "" {
		existing, hadExisting := reasoningItems[itemID]
		if hadExisting {
			var warnings []types.Warning
			if len(summaryParts) > 0 {
				existing["summary"] = appendReasoningSummary(existing["summary"], summaryParts)
			} else {
				// Row (P1-5c item 7): TS warns (but still forwards
				// encrypted_content below) when an empty-text reasoning part
				// would otherwise append nothing to an already-started
				// reasoning sequence.
				raw, _ := jsonstr.Marshal(part)
				warnings = append(warnings, types.Warning{
					Type:    "other",
					Message: fmt.Sprintf("Cannot append empty reasoning part to existing reasoning sequence. Skipping reasoning part: %s.", raw),
				})
			}
			if part.EncryptedContent != "" {
				existing["encrypted_content"] = part.EncryptedContent
			}
			return nil, warnings
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
		return item, nil
	}
	if part.EncryptedContent == "" {
		// Row (P1-5c item 7): a reasoning part with neither an itemId nor
		// encrypted_content didn't originate from this provider and cannot
		// be replayed; TS warns instead of silently dropping it.
		raw, _ := jsonstr.Marshal(part)
		return nil, []types.Warning{{
			Type:    "other",
			Message: fmt.Sprintf("Non-OpenAI reasoning parts are not supported. Skipping reasoning part: %s.", raw),
		}}
	}

	item := map[string]interface{}{"type": "reasoning"}
	item["encrypted_content"] = part.EncryptedContent
	item["summary"] = summaryParts
	return item, nil
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

func filterReasoningItemsWithoutEncryptedContent(items []interface{}) ([]interface{}, []types.Warning) {
	filtered := items[:0]
	var warnings []types.Warning
	dropped := false
	for _, item := range items {
		reasoning, ok := item.(map[string]interface{})
		if ok && reasoning["type"] == "reasoning" && reasoning["encrypted_content"] == nil {
			dropped = true
			continue
		}
		filtered = append(filtered, item)
	}
	if dropped {
		warnings = append(warnings, types.Warning{
			Type:    "other",
			Message: "Reasoning parts without encrypted content are not supported when store is false. Skipping reasoning parts.",
		})
	}
	return filtered, warnings
}

// droppedToolResultWarning returns the TS "not sent to the API when store is
// false" warning for a provider-executed tool result that
// convertAssistantToolResultItem could not replay. It mirrors the tail of the
// TS tool-result case: tool search, programmatic tool calling and shell
// results are handled (or intentionally omitted) without a warning.
func droppedToolResultWarning(part types.ToolResultContent, opts ConvertOptions) *types.Warning {
	if opts.Store || opts.HasConversation || isExecutionDeniedOutput(part.Output) {
		return nil
	}
	if part.ToolName == opts.toolSearchName() || opts.providerToolName(part.ToolName) == "programmatic_tool_calling" {
		return nil
	}
	if opts.HasShellTool && opts.providerToolName(part.ToolName) == "shell" {
		return nil
	}
	return &types.Warning{
		Type:    "other",
		Message: fmt.Sprintf("Results for OpenAI tool %s are not sent to the API when store is false", part.ToolName),
	}
}

func convertAssistantToolResultItem(part types.ToolResultContent, opts ConvertOptions) (interface{}, error) {
	if isExecutionDeniedOutput(part.Output) {
		return nil, nil
	}
	if opts.HasConversation {
		return nil, nil
	}
	toolName := opts.providerToolName(part.ToolName)
	if isWebSearchToolName(part.ToolName, opts) {
		// TS #22346: stateless replay re-creates the web_search_call so the
		// search context survives across steps.
		itemID := firstNonEmpty(openAIToolResultItemID(part, openAIProviderOptionsName(opts)), part.ToolCallID)
		if opts.Store {
			return map[string]interface{}{"type": "item_reference", "id": itemID}, nil
		}
		if call := convertWebSearchToolResultOutput(part, itemID); call != nil {
			return call, nil
		}
	}
	if part.ToolName == opts.toolSearchName() {
		itemID := firstNonEmpty(openAIToolResultItemID(part, openAIProviderOptionsName(opts)), part.ToolCallID)
		if opts.Store {
			return map[string]interface{}{
				"type": "item_reference",
				"id":   itemID,
			}, nil
		}
		if part.Output != nil && part.Output.Type == types.ToolResultOutputJSON {
			item, err := convertToolSearchOutput(part, "server")
			if err != nil {
				return nil, err
			}
			if output, ok := item.(ToolSearchOutputItem); ok {
				output.ID = itemID
				output.CallID = nil
				return output, nil
			}
		}
		return nil, nil
	}
	if opts.providerToolName(part.ToolName) == "programmatic_tool_calling" {
		// Row 1f6dd3a: the result of a hosted program's execution, mirroring
		// tool_search's item_reference-first handling above.
		itemID := firstNonEmpty(openAIToolResultItemID(part, openAIProviderOptionsName(opts)), part.ToolCallID)
		if opts.Store {
			return map[string]interface{}{
				"type": "item_reference",
				"id":   itemID,
			}, nil
		}
		if part.Output != nil && part.Output.Type == types.ToolResultOutputJSON {
			var parsed struct {
				Result string `json:"result"`
				Status string `json:"status"`
			}
			if err := decodeStrict(programOutputNode, part.Output.Value, &parsed); err != nil {
				return nil, err
			}
			return ProgramOutputItem{
				Type:   "program_output",
				ID:     itemID,
				CallID: part.ToolCallID,
				Result: parsed.Result,
				Status: parsed.Status,
			}, nil
		}
		return nil, nil
	}
	if opts.HasShellTool && toolName == "shell" {
		if part.Output != nil && part.Output.Type == types.ToolResultOutputJSON {
			return convertShellOutput(part)
		}
		return nil, nil
	}
	itemID := firstNonEmpty(openAIToolResultItemID(part, openAIProviderOptionsName(opts)), part.ToolCallID)
	if opts.Store && itemID != "" {
		return map[string]interface{}{
			"type": "item_reference",
			"id":   itemID,
		}, nil
	}
	return nil, nil
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

// ValidReasoningEffortUpdateValues is the fixed set of values TS validates
// reasoningEffortUpdate against at the schema level (both request-level
// providerOptions.openai.reasoningEffortUpdate and message-level
// providerOptions[provider].reasoningEffortUpdate on system messages), via
// z.enum(['none','low','medium','high','xhigh','max']) as of row
// 94d5d6d3e6. This is independent of, and checked before, any per-model
// SupportedReasoningEfforts restriction (e.g. gpt-6-astra rejecting "none").
var ValidReasoningEffortUpdateValues = []string{"none", "low", "medium", "high", "xhigh", "max"}

// isValidReasoningEffortUpdateValue reports whether effort is one of the
// fixed schema-level reasoningEffortUpdate values.
func isValidReasoningEffortUpdateValue(effort string) bool {
	for _, v := range ValidReasoningEffortUpdateValues {
		if v == effort {
			return true
		}
	}
	return false
}

func openAIProviderOptionsName(opts ConvertOptions) string {
	if opts.ProviderOptionsName != "" {
		return opts.ProviderOptionsName
	}
	return "openai"
}

// resolveProviderReferenceStrict mirrors TS resolveProviderReference: a
// reference that names other providers but not providerName is an error
// (NoSuchProviderReferenceError) instead of silently borrowing another
// provider's file ID. An empty reference, or the legacy single-ID form keyed
// by "", resolves without error.
func resolveProviderReferenceStrict(reference types.ProviderReference, providerName string) (string, error) {
	if len(reference) == 0 {
		return "", nil
	}
	if ref := reference[providerName]; ref != "" {
		return ref, nil
	}
	if ref := reference[""]; ref != "" && len(reference) == 1 {
		return ref, nil
	}
	return "", &providererrors.NoSuchProviderReferenceError{Provider: providerName, Reference: reference}
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
func convertToolItems(msg types.Message, options ...ConvertOptions) ([]interface{}, error) {
	var opts ConvertOptions
	if len(options) > 0 {
		opts = options[0]
	}
	items := make([]interface{}, 0, len(msg.Content))
	processedApprovals := opts.processedApprovalIDs
	if processedApprovals == nil {
		processedApprovals = map[string]bool{}
	}
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.ToolApprovalResponseContent:
			if p.ApprovalID == "" || processedApprovals[p.ApprovalID] {
				continue
			}
			processedApprovals[p.ApprovalID] = true
			if opts.Store && !opts.HasConversation && !opts.HasPreviousResponseID {
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
			if opts.Store && !opts.HasConversation && !opts.HasPreviousResponseID {
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
			if handled, err := appendParallelToolResult(&items, p, opts); handled {
				if err != nil {
					return nil, err
				}
				continue
			}
			if shouldSkipApprovalDeniedOutput(p) {
				continue
			}
			item, err := convertSpecialToolOutput(p, opts)
			if err != nil {
				return nil, err
			}
			if item != nil {
				items = append(items, item)
				continue
			}
			if isOpenAICustomToolName(p.ToolName, opts.CustomToolNames) {
				customOpts := opts
				customOpts.customToolOutput = true
				items = append(items, CustomToolCallOutput{
					Type:   "custom_tool_call_output",
					CallID: p.ToolCallID,
					Output: toolResultOutputWithOptions(p, customOpts),
				})
				continue
			}
			if err := rejectDeniedProgrammaticToolResult(p, opts); err != nil {
				return nil, err
			}
			items = append(items, FunctionCallOutputItem{
				Type:   "function_call_output",
				CallID: p.ToolCallID,
				Output: toolResultOutputWithOptions(p, opts),
				Caller: toolResultCaller(p, openAIProviderOptionsName(opts)),
			})
			if err := appendAdditionalTools(&items, p, opts); err != nil {
				return nil, err
			}
		case *types.ToolResultContent:
			if p != nil {
				if handled, err := appendParallelToolResult(&items, *p, opts); handled {
					if err != nil {
						return nil, err
					}
					continue
				}
				if shouldSkipApprovalDeniedOutput(*p) {
					continue
				}
				item, err := convertSpecialToolOutput(*p, opts)
				if err != nil {
					return nil, err
				}
				if item != nil {
					items = append(items, item)
					continue
				}
				if isOpenAICustomToolName(p.ToolName, opts.CustomToolNames) {
					customOpts := opts
					customOpts.customToolOutput = true
					items = append(items, CustomToolCallOutput{
						Type:   "custom_tool_call_output",
						CallID: p.ToolCallID,
						Output: toolResultOutputWithOptions(*p, customOpts),
					})
					continue
				}
				if err := rejectDeniedProgrammaticToolResult(*p, opts); err != nil {
					return nil, err
				}
				items = append(items, FunctionCallOutputItem{
					Type:   "function_call_output",
					CallID: p.ToolCallID,
					Output: toolResultOutputWithOptions(*p, opts),
					Caller: toolResultCaller(*p, openAIProviderOptionsName(opts)),
				})
				if err := appendAdditionalTools(&items, *p, opts); err != nil {
					return nil, err
				}
			}
		}
	}
	return items, nil
}

// appendAdditionalTools appends an `additional_tools` item right after the
// function_call_output of part when providerOptions.<name>.additionalTools
// lists function definitions (TS openaiResponsesToolResultOptionsSchema). The
// option is validated like the TS strict schema: every entry needs
// type "function", a non-empty name and a parameters object; description and
// strict are optional; unknown keys are rejected.
func appendAdditionalTools(items *[]interface{}, part types.ToolResultContent, opts ConvertOptions) error {
	providerName := openAIProviderOptionsName(opts)
	openaiOptions, ok := part.ProviderOptions[providerName].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, present := openaiOptions["additionalTools"]
	if !present {
		return nil
	}
	field := "providerOptions." + providerName + ".additionalTools"
	invalid := func(message string) error {
		return &providererrors.InvalidArgumentError{Field: field, Message: message}
	}
	if raw == nil {
		return invalid("expected an array of function tool definitions")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return invalid(err.Error())
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(encoded, &entries); err != nil {
		return invalid("expected an array of function tool definitions")
	}
	tools := make([]AdditionalToolDef, 0, len(entries))
	for i, entry := range entries {
		var def struct {
			Type        *string                `json:"type"`
			Name        *string                `json:"name"`
			Description *string                `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
			Strict      *bool                  `json:"strict"`
		}
		decoder := json.NewDecoder(bytes.NewReader(entry))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&def); err != nil {
			return invalid(fmt.Sprintf("entry %d is not a valid function tool definition: %v", i, err))
		}
		switch {
		case def.Type == nil || *def.Type != "function":
			return invalid(fmt.Sprintf("entry %d: type must be \"function\"", i))
		case def.Name == nil || *def.Name == "":
			return invalid(fmt.Sprintf("entry %d: name must be a non-empty string", i))
		case def.Parameters == nil:
			return invalid(fmt.Sprintf("entry %d: parameters must be an object", i))
		}
		tools = append(tools, AdditionalToolDef{
			Type:        "function",
			Name:        *def.Name,
			Description: def.Description,
			Parameters:  def.Parameters,
			Strict:      def.Strict,
		})
	}
	if len(tools) == 0 {
		return nil
	}
	*items = append(*items, AdditionalToolsItem{Type: "additional_tools", Role: "developer", Tools: tools})
	return nil
}

// appendParallelToolResult reports whether part belongs to a complete
// parallel tool result group (row 6be0f51): if so, it appends the group's
// single regrouped function_call_output to items (only on the first child
// result seen for that group) and returns handled=true so the caller skips
// its normal per-part handling for this part.
func appendParallelToolResult(items *[]interface{}, part types.ToolResultContent, opts ConvertOptions) (handled bool, err error) {
	metadata, ok := GetParallelToolCallMetadata(toolResultContentProviderMetadata(part), openAIProviderOptionsName(opts))
	if !ok {
		return false, nil
	}
	group, exists := opts.parallelToolResultGroups[metadata.ToolCallID]
	if !exists || !sameParallelToolCall(group.metadata, metadata) {
		return false, nil
	}
	if !opts.emittedParallelToolResults[group.metadata.ToolCallID] {
		opts.emittedParallelToolResults[group.metadata.ToolCallID] = true
		*items = append(*items, buildParallelFunctionCallOutput(group, opts))
	}
	return true, nil
}

// collectProgrammaticCallerIDs scans the prompt's assistant messages for
// tool calls whose caller was a programmatic-tool-calling "program",
// returning their tool-call-ids. Used as a fallback by
// rejectDeniedProgrammaticToolResult when a tool-result doesn't carry its
// own caller metadata (row e105b2b, mirrors TS's programmaticToolCallIds).
func collectProgrammaticCallerIDs(prompt types.Prompt, providerName string) map[string]bool {
	ids := map[string]bool{}
	addIfProgrammatic := func(toolCallID string, metadata map[string]interface{}) {
		if toolCallID == "" {
			return
		}
		if caller := toolCallCallerFromMetadata(metadata, providerName); caller != nil && caller.Type == "program" {
			ids[toolCallID] = true
		}
	}
	for _, msg := range prompt.Messages {
		if msg.Role != types.RoleAssistant {
			continue
		}
		for _, tc := range msg.ToolCalls {
			addIfProgrammatic(tc.ID, tc.ProviderMetadata)
		}
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.ToolCallContent:
				addIfProgrammatic(p.ToolCallID, toolCallContentProviderMetadata(p))
			case *types.ToolCallContent:
				if p != nil {
					addIfProgrammatic(p.ToolCallID, toolCallContentProviderMetadata(*p))
				}
			}
		}
	}
	return ids
}

// rejectDeniedProgrammaticToolResult mirrors TS's check in the 'tool' role
// case: a client function tool invoked by a programmatic-tool-calling
// "program" caller cannot receive an execution-denied result, since there is
// no interactive approval loop inside the hosted JavaScript sandbox (row
// e105b2b).
func rejectDeniedProgrammaticToolResult(part types.ToolResultContent, opts ConvertOptions) error {
	if !isExecutionDeniedOutput(part.Output) {
		return nil
	}
	providerName := openAIProviderOptionsName(opts)
	caller := toolResultCaller(part, providerName)
	isProgrammatic := (caller != nil && caller.Type == "program") || opts.programmaticCallerIDs[part.ToolCallID]
	if !isProgrammatic {
		return nil
	}
	// Matches TS's UnsupportedFunctionalityError({ functionality:
	// 'execution-denied results for programmatic tool calls' }), whose
	// default message template is `'${functionality}' functionality not
	// supported.`.
	const functionality = "execution-denied results for programmatic tool calls"
	return &providererrors.UnsupportedFunctionalityError{
		Functionality: functionality,
		Message:       fmt.Sprintf("'%s' functionality not supported.", functionality),
	}
}

// sameParallelToolCall reports whether two ParallelToolCallMetadata values
// describe the same "parallel" wrapper call (row 6be0f51).
func sameParallelToolCall(a, b ParallelToolCallMetadata) bool {
	return a.ItemID == b.ItemID &&
		a.ToolCallID == b.ToolCallID &&
		a.ToolName == b.ToolName &&
		a.Input == b.Input &&
		a.Count == b.Count
}

// toolResultContentProviderMetadata merges a ToolResultContent's
// ProviderOptions and ProviderMetadata into one map, keyed by provider name,
// mirroring toolCallContentProviderMetadata for the tool-result side.
func toolResultContentProviderMetadata(part types.ToolResultContent) map[string]interface{} {
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

// collectCompleteParallelToolResultGroups scans the prompt's "tool" role
// messages for results carrying parallelToolCall metadata, and returns the
// complete groups: every child result for a given wrapper call_id present,
// none duplicated, and all agreeing on the same wrapper identity (row
// 6be0f51, mirrors TS collectCompleteParallelToolResultGroups).
func collectCompleteParallelToolResultGroups(prompt types.Prompt, providerName string) map[string]parallelToolResultGroup {
	type pendingGroup struct {
		metadata ParallelToolCallMetadata
		results  map[int]types.ToolResultContent
		invalid  bool
	}
	pending := map[string]*pendingGroup{}

	addResult := func(part types.ToolResultContent) {
		metadata, ok := GetParallelToolCallMetadata(toolResultContentProviderMetadata(part), providerName)
		if !ok {
			return
		}
		g, exists := pending[metadata.ToolCallID]
		if !exists {
			pending[metadata.ToolCallID] = &pendingGroup{
				metadata: metadata,
				results:  map[int]types.ToolResultContent{metadata.Index: part},
			}
			return
		}
		if _, has := g.results[metadata.Index]; has || !sameParallelToolCall(g.metadata, metadata) {
			g.invalid = true
			return
		}
		g.results[metadata.Index] = part
	}

	for _, msg := range prompt.Messages {
		if msg.Role != types.RoleTool {
			continue
		}
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.ToolResultContent:
				addResult(p)
			case *types.ToolResultContent:
				if p != nil {
					addResult(*p)
				}
			}
		}
	}

	complete := map[string]parallelToolResultGroup{}
	for toolCallID, g := range pending {
		if g.invalid || len(g.results) != g.metadata.Count {
			continue
		}
		ordered := make([]types.ToolResultContent, g.metadata.Count)
		allPresent := true
		for i := 0; i < g.metadata.Count; i++ {
			r, has := g.results[i]
			if !has {
				allPresent = false
				break
			}
			ordered[i] = r
		}
		if allPresent {
			complete[toolCallID] = parallelToolResultGroup{metadata: g.metadata, results: ordered}
		}
	}
	return complete
}

// buildParallelFunctionCallOutput joins a complete parallel tool result
// group's child outputs into the single function_call_output the server
// expects for its original "parallel" wrapper call_id, in the same order as
// the original tool_uses array (row 6be0f51).
func buildParallelFunctionCallOutput(group parallelToolResultGroup, opts ConvertOptions) FunctionCallOutputItem {
	providerName := openAIProviderOptionsName(opts)
	parts := make([]string, len(group.results))
	breakpoints := make([]interface{}, len(group.results))
	hasBreakpoint := false
	for i, r := range group.results {
		breakpoints[i] = opts.scalarToolResultBreakpoint(r, providerName)
		hasBreakpoint = hasBreakpoint || breakpoints[i] != nil
		switch out := toolResultOutputBP(r, opts, false).(type) {
		case string:
			parts[i] = out
		default:
			b, _ := jsonstr.Marshal(out)
			parts[i] = string(b)
		}
	}
	// TS: with any child breakpoint the output is an array of input_text
	// parts (one per child, "\n"-prefixed after the first), each carrying its
	// own breakpoint; otherwise the children are joined with "\n".
	if hasBreakpoint {
		out := make([]CustomToolCallOutputPart, len(parts))
		for i, text := range parts {
			if i > 0 {
				text = "\n" + text
			}
			out[i] = CustomToolCallOutputPart{Type: "input_text", Text: text, PromptCacheBreakpoint: breakpoints[i]}
		}
		return FunctionCallOutputItem{Type: "function_call_output", CallID: group.metadata.ToolCallID, Output: out}
	}
	return FunctionCallOutputItem{
		Type:   "function_call_output",
		CallID: group.metadata.ToolCallID,
		Output: strings.Join(parts, "\n"),
	}
}

func convertSpecialToolOutput(part types.ToolResultContent, opts ConvertOptions) (interface{}, error) {
	if part.Output == nil || part.Output.Type != types.ToolResultOutputJSON {
		return nil, nil
	}
	if part.ToolName == opts.toolSearchName() {
		return convertToolSearchOutput(part, "client")
	}
	toolName := opts.providerToolName(part.ToolName)
	switch toolName {
	case "local_shell":
		if !opts.HasLocalShellTool {
			return nil, nil
		}
		return convertLocalShellOutput(part)
	case "shell":
		if !opts.HasShellTool {
			return nil, nil
		}
		return convertShellOutput(part)
	case "apply_patch":
		// TS matches the RAW tool name here (part.toolName === 'apply_patch'),
		// not the provider-mapped one. The Go factory (NewApplyPatchTool) names
		// the tool "openai.apply_patch", so that default name is the raw name
		// of a Go caller's tool and is accepted too; a custom name mapped onto
		// apply_patch is not.
		if !opts.HasApplyPatchTool || (part.ToolName != "apply_patch" && part.ToolName != "openai.apply_patch") {
			return nil, nil
		}
		return convertApplyPatchOutput(part)
	case "computer":
		if !opts.HasComputerTool {
			return nil, nil
		}
		return convertComputerOutput(part)
	default:
		return nil, nil
	}
}

func convertToolSearchOutput(part types.ToolResultContent, execution string) (interface{}, error) {
	var parsed struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := decodeStrict(toolSearchOutputNode, part.Output.Value, &parsed); err != nil {
		return nil, err
	}
	var callID *string
	if execution == "client" {
		callID = &part.ToolCallID
	}
	// TS always sends `tools` (an empty array when nothing was loaded).
	if parsed.Tools == nil {
		parsed.Tools = []json.RawMessage{}
	}
	return ToolSearchOutputItem{
		Type:      "tool_search_output",
		Execution: execution,
		CallID:    callID,
		Status:    "completed",
		Tools:     parsed.Tools,
	}, nil
}

func convertLocalShellOutput(part types.ToolResultContent) (interface{}, error) {
	var parsed struct {
		Output string `json:"output"`
	}
	if err := decodeStrict(localShellOutputNode, part.Output.Value, &parsed); err != nil {
		return nil, err
	}
	return LocalShellCallOutput{
		Type:   "local_shell_call_output",
		CallID: part.ToolCallID,
		Output: parsed.Output,
	}, nil
}

func convertShellOutput(part types.ToolResultContent) (interface{}, error) {
	var parsed struct {
		MaxOutputLength *int `json:"maxOutputLength"`
		Output          []struct {
			Stdout  string `json:"stdout"`
			Stderr  string `json:"stderr"`
			Outcome struct {
				Type     string `json:"type"`
				ExitCode *int   `json:"exitCode"`
			} `json:"outcome"`
		} `json:"output"`
	}
	if err := decodeStrict(shellOutputNode, part.Output.Value, &parsed); err != nil {
		return nil, err
	}
	output := make([]ShellCallOutputEntry, 0, len(parsed.Output))
	for _, entry := range parsed.Output {
		outcome := ShellOutcome{Type: entry.Outcome.Type}
		if entry.Outcome.Type == "exit" {
			outcome.ExitCode = entry.Outcome.ExitCode
		}
		output = append(output, ShellCallOutputEntry{Stdout: entry.Stdout, Stderr: entry.Stderr, Outcome: outcome})
	}
	return ShellCallOutput{
		Type:            "shell_call_output",
		CallID:          part.ToolCallID,
		MaxOutputLength: parsed.MaxOutputLength,
		Output:          output,
	}, nil
}

func convertComputerOutput(part types.ToolResultContent) (interface{}, error) {
	var parsed struct {
		Output struct {
			ImageURL string `json:"imageUrl"`
			FileID   string `json:"fileId"`
			Detail   string `json:"detail"`
		} `json:"output"`
		AcknowledgedSafetyChecks []struct {
			ID      string `json:"id"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"acknowledgedSafetyChecks"`
	}
	if err := decodeStrict(computerOutputNode, part.Output.Value, &parsed); err != nil {
		return nil, err
	}
	checks := make([]ComputerSafetyCheck, 0, len(parsed.AcknowledgedSafetyChecks))
	for _, c := range parsed.AcknowledgedSafetyChecks {
		checks = append(checks, ComputerSafetyCheck{ID: c.ID, Code: c.Code, Message: c.Message})
	}
	return ComputerCallOutput{
		Type:   "computer_call_output",
		CallID: part.ToolCallID,
		Output: ComputerCallOutputScreenshot{
			Type:     "computer_screenshot",
			ImageURL: parsed.Output.ImageURL,
			FileID:   parsed.Output.FileID,
			Detail:   parsed.Output.Detail,
		},
		AcknowledgedSafetyChecks: checks,
	}, nil
}

func convertApplyPatchOutput(part types.ToolResultContent) (interface{}, error) {
	var parsed struct {
		Status string  `json:"status"`
		Output *string `json:"output"`
	}
	if err := decodeStrict(applyPatchOutputNode, part.Output.Value, &parsed); err != nil {
		return nil, err
	}
	return ApplyPatchCallOutput{
		Type:   "apply_patch_call_output",
		CallID: part.ToolCallID,
		Status: parsed.Status,
		Output: parsed.Output,
	}, nil
}

// providerToolName resolves an SDK tool name to the bare OpenAI provider tool
// name, honouring custom names declared for provider tools.
func (o ConvertOptions) providerToolName(name string) string {
	if mapped, ok := o.ProviderToolNames[name]; ok {
		return mapped
	}
	return normalizeOpenAIToolName(name)
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

func localShellActionFromArgs(args map[string]interface{}) (LocalShellAction, error) {
	var parsed struct {
		Action struct {
			Command          []string          `json:"command"`
			TimeoutMs        *int              `json:"timeoutMs"`
			User             *string           `json:"user"`
			WorkingDirectory *string           `json:"workingDirectory"`
			Env              map[string]string `json:"env"`
		} `json:"action"`
	}
	if err := decodeStrict(localShellInputNode, args, &parsed); err != nil {
		return LocalShellAction{}, err
	}
	a := parsed.Action
	return LocalShellAction{
		Type:             "exec",
		Command:          a.Command,
		TimeoutMs:        a.TimeoutMs,
		User:             a.User,
		WorkingDirectory: a.WorkingDirectory,
		Env:              a.Env,
	}, nil
}

func shellActionFromArgs(args map[string]interface{}) (ShellCallAction, error) {
	var parsed struct {
		Action struct {
			Commands        []string `json:"commands"`
			TimeoutMs       *int     `json:"timeoutMs"`
			MaxOutputLength *int     `json:"maxOutputLength"`
		} `json:"action"`
	}
	if err := decodeStrict(shellInputNode, args, &parsed); err != nil {
		return ShellCallAction{}, err
	}
	return ShellCallAction{
		Commands:        parsed.Action.Commands,
		TimeoutMs:       parsed.Action.TimeoutMs,
		MaxOutputLength: parsed.Action.MaxOutputLength,
	}, nil
}

// computerCallFromArgs validates a computer tool call's input and returns its
// status, actions (field names translated to the wire format, row 0063c2d;
// keys zod strips are dropped) and pending safety checks.
func computerCallFromArgs(args map[string]interface{}) (string, []map[string]interface{}, []ComputerSafetyCheck, error) {
	if err := validateToolPayload(computerInputNode, args); err != nil {
		return "", nil, nil, err
	}
	status, _ := args["status"].(string)
	rawActions, _ := args["actions"].([]interface{})
	actions := make([]map[string]interface{}, 0, len(rawActions))
	for _, item := range rawActions {
		m, _ := item.(map[string]interface{})
		typ, _ := m["type"].(string)
		kept := make(map[string]interface{}, len(m))
		for _, k := range computerActionFields[typ] {
			if v, ok := m[k]; ok {
				kept[k] = v
			}
		}
		actions = append(actions, MapComputerActionToWire(kept))
	}
	rawChecks, _ := args["pendingSafetyChecks"].([]interface{})
	checks := make([]ComputerSafetyCheck, 0, len(rawChecks))
	for _, item := range rawChecks {
		m, _ := item.(map[string]interface{})
		checks = append(checks, ComputerSafetyCheck{
			ID:      stringArg(m, "id"),
			Code:    stringArg(m, "code"),
			Message: stringArg(m, "message"),
		})
	}
	return status, actions, checks, nil
}

func applyPatchFromArgs(args map[string]interface{}) (string, ApplyPatchOperation, error) {
	var parsed struct {
		CallID    string `json:"callId"`
		Operation struct {
			Type string  `json:"type"`
			Path string  `json:"path"`
			Diff *string `json:"diff"`
		} `json:"operation"`
	}
	if err := decodeStrict(applyPatchInputNode, args, &parsed); err != nil {
		return "", ApplyPatchOperation{}, err
	}
	op := parsed.Operation
	return parsed.CallID, ApplyPatchOperation{Type: op.Type, Path: op.Path, Diff: op.Diff}, nil
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

func decodeToolOutputJSON(value interface{}, target interface{}) bool {
	data, err := jsonstr.Marshal(value)
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
	// TS reads output.providerOptions.openai.approvalId (the denial result the
	// core builds for provider-executed tools); the part-level options are
	// also honoured for callers that set it there.
	for _, options := range []map[string]interface{}{part.Output.ProviderOptions, part.ProviderOptions} {
		openaiOptions, ok := options["openai"].(map[string]interface{})
		if !ok {
			continue
		}
		if approvalID, _ := openaiOptions["approvalId"].(string); approvalID != "" {
			return true
		}
	}
	return false
}

// validateToolOutputFileReferences mirrors TS resolveProviderReference for
// file blocks inside tool-result content: a reference naming other providers
// but not providerName is a NoSuchProviderReferenceError.
func validateToolOutputFileReferences(prompt types.Prompt, opts ConvertOptions) error {
	providerName := openAIProviderOptionsName(opts)
	for _, msg := range prompt.Messages {
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
			if tr.Output == nil {
				continue
			}
			if tr.Output.Type != types.ToolResultOutputContent {
				continue
			}
			// TS custom tool outputs have no reference branch.
			isCustom := isOpenAICustomToolName(tr.ToolName, opts.CustomToolNames)
			for _, block := range tr.Output.Content {
				b, ok := block.(types.FileContentBlock)
				if !ok {
					continue
				}
				if !isCustom && len(b.FileData.Reference) > 0 && (b.FileData.Type == types.FileDataTypeReference || b.FileData.Type == "") {
					if _, err := resolveProviderReferenceStrict(b.FileData.Reference, providerName); err != nil {
						return err
					}
				}
				// TS resolveFullMediaType throws for inline bytes whose bare or
				// wildcard media type cannot be detected.
				if toolResultBlockDataType(b) == types.FileDataTypeData {
					if data := fileContentBlockDataBase64(b); data != "" {
						if _, err := resolveInlineBase64MediaType(firstNonEmpty(b.MediaType, b.FileData.MediaType), data); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

// toolResultOutput converts a ToolResultContent to the Responses API output value.
// Returns a plain string for text/json outputs, or a []CustomToolCallOutputPart for
// content-array outputs (text, image-data, image-url, file-data, file-url).
func toolResultOutput(tr types.ToolResultContent) interface{} {
	return toolResultOutputWithOptions(tr, ConvertOptions{})
}

func toolResultOutputWithOptions(tr types.ToolResultContent, opts ConvertOptions) interface{} {
	return toolResultOutputBP(tr, opts, true)
}

// toolResultOutputBP is toolResultOutputWithOptions with control over scalar
// prompt cache breakpoints: when scalarBreakpoint is set, a text/json/error/
// denied output that carries a breakpoint becomes a one-element input_text
// array (TS convertScalarOutput). A regrouped parallel call turns it off and
// applies the breakpoints to its joined parts itself.
func toolResultOutputBP(tr types.ToolResultContent, opts ConvertOptions, scalarBreakpoint bool) interface{} {
	providerName := openAIProviderOptionsName(opts)
	var scalarBP interface{}
	if scalarBreakpoint {
		scalarBP = opts.scalarToolResultBreakpoint(tr, providerName)
	}
	asOutput := func(value interface{}) interface{} {
		text, isString := value.(string)
		if scalarBP == nil || !isString {
			return value
		}
		return []CustomToolCallOutputPart{{Type: "input_text", Text: text, PromptCacheBreakpoint: scalarBP}}
	}
	// OpenAI parses function_call_output.output as JSON when the function
	// declared output_schema. Text-like results therefore need to be
	// JSON-encoded to become valid JSON string literals.
	hasOutputSchema := opts.OutputSchemaToolNames[tr.ToolName]
	scalarOutput := func(value string) interface{} {
		if !hasOutputSchema {
			return value
		}
		b, _ := jsonstr.Marshal(value)
		return string(b)
	}
	if tr.Output != nil {
		switch tr.Output.Type {
		case types.ToolResultOutputErrorText, types.ToolResultOutputErrorJSON, types.ToolResultOutputError:
			// Tool errors are wrapped as {"error": value} so the model can
			// tell a failed execution from a successful result (TS #22209).
			// The wrapped text is already JSON, so it is not re-encoded for
			// tools with an output schema.
			if b, err := jsonstr.Marshal(map[string]interface{}{"error": tr.Output.Value}); err == nil {
				return asOutput(string(b))
			}
		case types.ToolResultOutputText:
			if s, ok := tr.Output.Value.(string); ok {
				return asOutput(scalarOutput(s))
			}
		case types.ToolResultOutputJSON:
			if b, err := jsonstr.Marshal(tr.Output.Value); err == nil {
				return asOutput(string(b))
			}
		case types.ToolResultOutputExecutionDenied:
			reason := tr.Output.Reason
			if reason == "" {
				reason = "Tool call execution denied."
			}
			return asOutput(scalarOutput(reason))
		case types.ToolResultOutputContent:
			parts := make([]CustomToolCallOutputPart, 0, len(tr.Output.Content))
			for _, block := range tr.Output.Content {
				partsBefore := len(parts)
				blockBP := opts.cacheBreakpoint(toolResultBlockProviderOptions(block), providerName)
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
					fileDataType := toolResultBlockDataType(b)
					switch fileDataType {
					case types.FileDataTypeReference:
						if opts.customToolOutput {
							// TS custom tool outputs have no reference branch.
							opts.warnToolOutput("unsupported tool content part type: file with data type: reference")
							continue
						}
						reference := firstNonEmpty(providerReferenceString(b.FileData.Reference, providerName), b.Reference)
						if reference == "" {
							continue
						}
						if isImageMediaType(mediaType) {
							parts = append(parts, CustomToolCallOutputPart{
								Type:   "input_image",
								FileID: reference,
								Detail: openAIResponsesImageDetail(b.ProviderOptions, providerName),
							})
						} else {
							parts = append(parts, CustomToolCallOutputPart{
								Type:   "input_file",
								FileID: reference,
							})
						}
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
						resolvedMediaType, resolveErr := resolveInlineBase64MediaType(mediaType, data)
						if resolveErr != nil {
							// This path cannot return an error; keep the declared type.
							resolvedMediaType = mediaType
						}
						fileData := fmt.Sprintf("data:%s;base64,%s", resolvedMediaType, data)
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
					default:
						if fileDataType != "" {
							opts.warnToolOutput(fmt.Sprintf("unsupported tool content part type: file with data type: %s", fileDataType))
						}
					}
				default:
					opts.warnToolOutput(fmt.Sprintf("unsupported tool content part type: %s", block.ToolResultContentType()))
				}
				if blockBP != nil && len(parts) > partsBefore {
					parts[partsBefore].PromptCacheBreakpoint = blockBP
				}
			}
			// TS maps and filters the content array, so an output whose parts
			// were all dropped is sent as an empty array.
			return parts
		}
		// TS custom tool outputs default to "" for an unknown output type.
		if opts.customToolOutput {
			return ""
		}
	}
	if s, ok := tr.Result.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", tr.Result)
}

// toolResultBlockDataType returns the block's explicit file data type, or the
// one implied by which payload field is set.
func toolResultBlockDataType(b types.FileContentBlock) types.FileDataType {
	if b.FileData.Type != "" {
		return b.FileData.Type
	}
	switch {
	case b.URL != "":
		return types.FileDataTypeURL
	case b.Reference != "" || len(b.FileData.Reference) > 0:
		return types.FileDataTypeReference
	case len(b.Data) > 0:
		return types.FileDataTypeData
	case b.FileData.DataString != "" || len(b.FileData.Data) > 0:
		return types.FileDataTypeData
	}
	return ""
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

func isWebSearchToolName(name string, opts ConvertOptions) bool {
	if opts.WebSearchToolName != "" && name == opts.WebSearchToolName {
		return true
	}
	switch normalizeOpenAIToolName(name) {
	case "web_search", "web_search_preview":
		return true
	}
	return false
}

// convertWebSearchToolResultOutput maps a stored web search tool result back
// to a web_search_call input item. It returns nil (so the generic handling
// applies) when the output is not a JSON object with a valid action,
// mirroring TS convertWebSearchToolResultOutput's schema validation.
func convertWebSearchToolResultOutput(part types.ToolResultContent, id string) interface{} {
	var value interface{}
	switch {
	case part.Output != nil:
		if part.Output.Type != types.ToolResultOutputJSON {
			return nil
		}
		value = part.Output.Value
	case part.Result != nil:
		value = part.Result
	default:
		return nil
	}
	var parsed map[string]interface{}
	if !decodeToolOutputJSON(value, &parsed) {
		return nil
	}
	action, ok := parsed["action"].(map[string]interface{})
	if !ok {
		return nil
	}
	optString := func(m map[string]interface{}, key string) (interface{}, bool, bool) {
		v, present := m[key]
		if !present {
			return nil, false, true
		}
		if v == nil {
			return nil, true, true
		}
		if str, ok := v.(string); ok {
			return str, true, true
		}
		return nil, false, false
	}
	// sources is a top-level member of the TS schema: an invalid entry fails
	// the validation whatever the action type, though only a search keeps it.
	var validSources []interface{}
	if rawSources, present := parsed["sources"]; present {
		list, ok := rawSources.([]interface{})
		if !ok {
			return nil
		}
		validSources = make([]interface{}, 0, len(list))
		for _, src := range list {
			m, ok := src.(map[string]interface{})
			if !ok {
				return nil
			}
			// z.object strips unknown keys, so only the schema's members
			// reach the wire.
			switch m["type"] {
			case "url":
				url, ok := m["url"].(string)
				if !ok {
					return nil
				}
				validSources = append(validSources, map[string]interface{}{"type": "url", "url": url})
			case "api":
				name, ok := m["name"].(string)
				if !ok {
					return nil
				}
				validSources = append(validSources, map[string]interface{}{"type": "api", "name": name})
			default:
				return nil
			}
		}
	}
	wireAction := map[string]interface{}{}
	switch action["type"] {
	case "search":
		wireAction["type"] = "search"
		// zod .optional() rejects null (only .nullish() accepts it).
		if v, present, ok := optString(action, "query"); !ok || (present && v == nil) {
			return nil
		} else if present {
			wireAction["query"] = v
		}
		if rawQueries, present := action["queries"]; present {
			list, ok := rawQueries.([]interface{})
			if !ok {
				return nil
			}
			queries := make([]string, 0, len(list))
			for _, q := range list {
				str, ok := q.(string)
				if !ok {
					return nil
				}
				queries = append(queries, str)
			}
			wireAction["queries"] = queries
		}
		if validSources != nil {
			wireAction["sources"] = validSources
		}
	case "openPage":
		wireAction["type"] = "open_page"
		v, present, ok := optString(action, "url")
		if !ok {
			return nil
		}
		if present {
			wireAction["url"] = v
		}
	case "findInPage":
		wireAction["type"] = "find_in_page"
		for _, key := range []string{"url", "pattern"} {
			v, present, ok := optString(action, key)
			if !ok {
				return nil
			}
			if present {
				wireAction[key] = v
			}
		}
	default:
		return nil
	}
	return map[string]interface{}{
		"type":   "web_search_call",
		"id":     id,
		"status": "completed",
		"action": wireAction,
	}
}

// promptCacheBreakpoint reads providerOptions[providerName].
// promptCacheBreakpoint (TS getPromptCacheBreakpoint): only the request's own
// provider-options key is consulted, and the value is forwarded verbatim.
func promptCacheBreakpoint(providerOptions map[string]interface{}, providerName string) interface{} {
	if providerOptions == nil {
		return nil
	}
	opts, ok := providerOptions[providerName].(map[string]interface{})
	if !ok {
		return nil
	}
	return opts["promptCacheBreakpoint"]
}

// withPromptCacheBreakpoint attaches a breakpoint to an already-built wire
// part (a typed Responses part or a plain map).
func withPromptCacheBreakpoint(part interface{}, bp interface{}) interface{} {
	if bp == nil {
		return part
	}
	switch p := part.(type) {
	case UserTextPart:
		p.PromptCacheBreakpoint = bp
		return p
	case UserImageURLPart:
		p.PromptCacheBreakpoint = bp
		return p
	case UserFilePart:
		p.PromptCacheBreakpoint = bp
		return p
	case map[string]interface{}:
		p["prompt_cache_breakpoint"] = bp
		return p
	}
	return part
}

// toolResultBlockProviderOptions returns the provider options of a tool result
// content block.
func toolResultBlockProviderOptions(block types.ToolResultContentBlock) map[string]interface{} {
	switch b := block.(type) {
	case types.TextContentBlock:
		return b.ProviderOptions
	case types.ImageContentBlock:
		return b.ProviderOptions
	case types.FileContentBlock:
		return b.ProviderOptions
	}
	return nil
}

// scalarToolResultPromptCacheBreakpoint mirrors TS
// getScalarToolResultPromptCacheBreakpoint: content outputs carry their
// breakpoints per part; every other output uses its own providerOptions, then
// the tool result part's.
func scalarToolResultPromptCacheBreakpoint(tr types.ToolResultContent, providerName string) interface{} {
	if tr.Output != nil {
		if tr.Output.Type == types.ToolResultOutputContent {
			return nil
		}
		if bp := promptCacheBreakpoint(tr.Output.ProviderOptions, providerName); bp != nil {
			return bp
		}
	}
	return promptCacheBreakpoint(tr.ProviderOptions, providerName)
}

// cacheBreakpoint is promptCacheBreakpoint gated on the converter option.
func (o ConvertOptions) cacheBreakpoint(providerOptions map[string]interface{}, providerName string) interface{} {
	if !o.IncludePromptCacheBreakpoint {
		return nil
	}
	return promptCacheBreakpoint(providerOptions, providerName)
}

// scalarToolResultBreakpoint is scalarToolResultPromptCacheBreakpoint gated on
// the converter option.
func (o ConvertOptions) scalarToolResultBreakpoint(tr types.ToolResultContent, providerName string) interface{} {
	if !o.IncludePromptCacheBreakpoint {
		return nil
	}
	return scalarToolResultPromptCacheBreakpoint(tr, providerName)
}
