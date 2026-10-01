package google

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// convertPrompt ports TS convertToGoogleInteractionsInput: the Interactions
// wire request body has an `input` field that is ALWAYS a flat array of
// discriminated step objects (`GoogleInteractionsInput = Array<
// GoogleInteractionsStep>`) — never a `{role, content}` "turn" wrapper, and
// never collapsed to a bare content array even for a single user message.
//   - a user message becomes one `user_input` step (content = its text/file
//     blocks, with adjacent text merged);
//   - a tool message becomes one `user_input` step whose content holds one
//     `function_result` block per tool-result part;
//   - an assistant message fans out into potentially several steps: adjacent
//     text/file content coalesces into one `model_output` step, while
//     reasoning, tool-calls, and processing_call/processing_result custom
//     parts each become their own top-level step (see convertAssistantSteps).
func (m *InteractionsLanguageModel) convertPrompt(p types.Prompt, opts GoogleInteractionsProviderOptions) ([]map[string]interface{}, string, []types.Warning, error) {
	warnings := make([]types.Warning, 0)
	messages := p.Messages
	if len(messages) == 0 && p.Text != "" {
		messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: p.Text}}}}
	}
	if p.System != "" {
		messages = append([]types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: p.System}}}}, messages...)
	}

	if opts.PreviousInteractionID != "" && opts.Store != nil && !*opts.Store {
		msg := "google.interactions: providerOptions.google.previousInteractionId was set together with store: false. These are incoherent (the prior interaction cannot be referenced when nothing was stored on the server); the full history will be sent and previous_interaction_id will still be emitted."
		warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
	} else if opts.PreviousInteractionID != "" {
		messages = compactMessagesForInteraction(messages, opts.PreviousInteractionID)
	}

	var systemTexts []string
	steps := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			for _, part := range msg.Content {
				if text, ok := part.(types.TextContent); ok {
					systemTexts = append(systemTexts, text.Text)
				}
			}
		case types.RoleUser:
			content, ws, err := m.convertContentParts(msg.Content, opts.MediaResolution)
			if err != nil {
				return nil, "", nil, err
			}
			warnings = append(warnings, ws...)
			content = mergeAdjacentInteractionText(content)
			if len(content) > 0 {
				steps = append(steps, map[string]interface{}{"type": "user_input", "content": content})
			}
		case types.RoleAssistant:
			assistantSteps, ws, err := m.convertAssistantSteps(msg.Content, msg.ToolCalls, opts.MediaResolution)
			if err != nil {
				return nil, "", nil, err
			}
			warnings = append(warnings, ws...)
			steps = append(steps, assistantSteps...)
		case types.RoleTool:
			content, ws, err := convertToolResults(msg.Content)
			if err != nil {
				return nil, "", nil, err
			}
			warnings = append(warnings, ws...)
			if len(content) > 0 {
				steps = append(steps, map[string]interface{}{"type": "user_input", "content": content})
			}
		}
	}

	systemInstruction := strings.Join(systemTexts, "\n\n")
	return steps, systemInstruction, warnings, nil
}

func (m *InteractionsLanguageModel) convertContentParts(parts []types.ContentPart, mediaResolution string) ([]map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	content := make([]map[string]interface{}, 0, len(parts))
	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			content = append(content, map[string]interface{}{"type": "text", "text": p.Text})
		case types.FileContent:
			block, ws, err := fileContentToInteractionBlock(p, mediaResolution)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, ws...)
			if block == nil {
				continue
			}
			content = append(content, block)
		case types.ImageContent:
			file := types.FileContent{Data: p.Image, MediaType: p.MimeType, MimeType: p.MimeType, URL: p.URL, ProviderOptions: p.ProviderOptions}
			block, ws, err := fileContentToInteractionBlock(file, mediaResolution)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, ws...)
			if block == nil {
				continue
			}
			content = append(content, block)
		}
	}
	return content, warnings, nil
}

// convertAssistantSteps ports TS's assistant-message branch of
// convertToGoogleInteractionsInput: adjacent text/file content blocks
// coalesce into a single `model_output` step (flushed whenever a
// non-coalescable part is encountered, and at the end), while reasoning,
// tool-calls, and processing_call/processing_result custom parts each
// become their own top-level step — never nested inside `model_output`.
//
// Tool calls may appear inline in `parts` (as types.ToolCallContent, for a
// hand-built prompt) and/or in the separate `toolCalls` slice (as populated
// by the SDK's own conversation history, see types.Message.ToolCalls). A
// call ID seen inline is not duplicated from `toolCalls`.
func (m *InteractionsLanguageModel) convertAssistantSteps(parts []types.ContentPart, toolCalls []types.ToolCall, mediaResolution string) ([]map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	var steps []map[string]interface{}
	var pending []map[string]interface{}
	seenToolCallIDs := map[string]bool{}

	flush := func() {
		if len(pending) > 0 {
			steps = append(steps, map[string]interface{}{"type": "model_output", "content": pending})
			pending = nil
		}
	}

	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			pending = append(pending, map[string]interface{}{"type": "text", "text": p.Text})
		case types.ReasoningContent:
			flush()
			steps = append(steps, pruneMap(map[string]interface{}{
				"type":      "thought",
				"signature": emptyToNil(firstNonEmpty(p.Signature, googleSignature(p.ProviderMetadata))),
				"summary":   reasoningSummary(p.Text),
			}))
		case types.FileContent:
			block, ws, err := fileContentToInteractionBlock(p, mediaResolution)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, ws...)
			if block == nil {
				continue
			}
			pending = append(pending, block)
		case types.ToolCallContent:
			flush()
			if p.ToolCallID != "" {
				seenToolCallIDs[p.ToolCallID] = true
			}
			args := p.Arguments
			if args == nil && p.Input != "" {
				_ = json.Unmarshal([]byte(p.Input), &args)
			}
			if args == nil {
				args = map[string]interface{}{}
			}
			steps = append(steps, pruneMap(map[string]interface{}{
				"type":      "function_call",
				"id":        p.ToolCallID,
				"name":      p.ToolName,
				"arguments": args,
				"signature": emptyToNil(firstNonEmpty(p.ThoughtSignature, signatureFromRawMetadata(p.ProviderMetadata))),
			}))
		case types.CustomContent:
			flush()
			google := extractGoogleMetadata(p.ProviderMetadata)
			signature, _ := google["signature"].(string)
			switch p.Kind {
			case "google.processing_call":
				if processingID, _ := google["processingId"].(string); processingID != "" {
					steps = append(steps, pruneMap(map[string]interface{}{
						"type":      "processing_call",
						"id":        processingID,
						"signature": emptyToNil(signature),
					}))
					continue
				}
			case "google.processing_result":
				if processingCallID, _ := google["processingCallId"].(string); processingCallID != "" {
					steps = append(steps, pruneMap(map[string]interface{}{
						"type":      "processing_result",
						"call_id":   processingCallID,
						"signature": emptyToNil(signature),
					}))
					continue
				}
			}
			msg := fmt.Sprintf("google.interactions: unsupported or invalid custom assistant content part %q; part dropped.", p.Kind)
			warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
		default:
			msg := fmt.Sprintf("google.interactions: unsupported assistant content part type %q; part dropped.", part.ContentType())
			warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
		}
	}
	for _, call := range toolCalls {
		if call.ID != "" && seenToolCallIDs[call.ID] {
			continue
		}
		flush()
		args := call.Arguments
		if args == nil {
			args = map[string]interface{}{}
		}
		steps = append(steps, pruneMap(map[string]interface{}{
			"type":      "function_call",
			"id":        call.ID,
			"name":      call.ToolName,
			"arguments": args,
			"signature": emptyToNil(firstNonEmpty(call.ThoughtSignature, signatureFromMetadataMap(call.ProviderMetadata))),
		}))
	}
	flush()
	return steps, warnings, nil
}

func fileContentToInteractionBlock(part types.FileContent, mediaResolution string) (map[string]interface{}, []types.Warning, error) {
	mediaType := firstNonEmpty(part.MediaType, part.MimeType, part.FileData.MediaType)
	kind := interactionMediaKind(mediaType)
	if part.FileData.Type == types.FileDataTypeText || part.Text != "" {
		return map[string]interface{}{"type": "text", "text": firstNonEmpty(part.FileData.Text, part.Text)}, nil, nil
	}
	if kind == "" {
		msg := fmt.Sprintf("google.interactions: unsupported file media type %q; part dropped.", mediaType)
		return nil, []types.Warning{{Type: "other", Message: msg, Details: msg}}, nil
	}
	var warnings []types.Warning
	block := map[string]interface{}{
		"type": kind,
	}
	switch {
	case part.FileData.Type == types.FileDataTypeData:
		block["data"] = encodeData(part.FileData.Data, part.FileData.DataString)
		block["mime_type"] = mediaType
	case len(part.Data) > 0:
		block["data"] = base64.StdEncoding.EncodeToString(part.Data)
		block["mime_type"] = mediaType
	case part.FileData.Type == types.FileDataTypeURL:
		block["uri"] = part.FileData.URL
		if isFullMediaType(mediaType) {
			block["mime_type"] = mediaType
		}
	case part.URL != "":
		block["uri"] = part.URL
		if isFullMediaType(mediaType) {
			block["mime_type"] = mediaType
		}
	case part.FileData.Type == types.FileDataTypeReference:
		block["uri"] = resolveGoogleReference(part.FileData.Reference)
		if isFullMediaType(mediaType) {
			block["mime_type"] = mediaType
		}
	case part.Reference != "":
		block["uri"] = part.Reference
		if isFullMediaType(mediaType) {
			block["mime_type"] = mediaType
		}
	default:
		block["data"] = ""
		block["mime_type"] = mediaType
	}
	if mediaResolution != "" && (kind == "image" || kind == "video") {
		block["resolution"] = mediaResolution
	}
	if kind == "video" {
		processing, processingWarning := videoProcessingField(part.ProviderOptions)
		if processing != nil {
			block["processing"] = processing
		}
		if processingWarning != nil {
			warnings = append(warnings, *processingWarning)
		}
	}
	return block, warnings, nil
}

// videoProcessingField converts providerOptions.google.processing (TS
// getVideoProcessingField) into the wire `processing` field for a video file
// part: either the strings "agentic"/"static", or
// `{type:"static", start_offset?, end_offset?, fps?}`. Returns (nil, nil)
// when unset, and (nil, warning) for an invalid value (option dropped, block
// still emitted).
func videoProcessingField(providerOptions map[string]interface{}) (interface{}, *types.Warning) {
	google, _ := providerOptions["google"].(map[string]interface{})
	if google == nil {
		return nil, nil
	}
	processing, ok := google["processing"]
	if !ok || processing == nil {
		return nil, nil
	}
	if s, ok := processing.(string); ok && (s == "agentic" || s == "static") {
		return s, nil
	}
	if m, ok := processing.(map[string]interface{}); ok && stringValue(m["type"]) == "static" {
		out := pruneMap(map[string]interface{}{
			"type":         "static",
			"start_offset": videoOffsetString(m["startOffset"]),
			"end_offset":   videoOffsetString(m["endOffset"]),
			"fps":          numericOrNil(m["fps"]),
		})
		if out == nil {
			out = map[string]interface{}{"type": "static"}
		}
		return out, nil
	}
	msg := "google.interactions: invalid providerOptions.google.processing on video file part; expected \"agentic\", \"static\", or a static processing configuration. Option dropped."
	return nil, &types.Warning{Type: "other", Message: msg, Details: msg}
}

// numericOrNil returns v when it is a JSON number type, otherwise nil (so
// pruneMap drops the key).
func numericOrNil(v interface{}) interface{} {
	switch v.(type) {
	case int, int32, int64, float32, float64:
		return v
	default:
		return nil
	}
}

// videoOffsetString formats a numeric video start/end offset as a duration
// string like "10.5s", mirroring TS `${config.startOffset}s` (a template
// literal, which coerces the number via JS's Number-to-string algorithm).
// strconv.FormatFloat with precision -1 (shortest round-tripping decimal, no
// exponential notation for this typical offset range) matches that output
// for realistic values (no trailing zeros, no redundant decimal point for
// whole numbers). Returns nil for non-numeric values so pruneMap drops the
// key, matching TS's `typeof config.startOffset === 'number'` guard.
func videoOffsetString(v interface{}) interface{} {
	var f float64
	switch n := v.(type) {
	case int:
		f = float64(n)
	case int32:
		f = float64(n)
	case int64:
		f = float64(n)
	case float32:
		f = float64(n)
	case float64:
		f = n
	default:
		return nil
	}
	return strconv.FormatFloat(f, 'f', -1, 64) + "s"
}

func convertToolResults(parts []types.ContentPart) ([]map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	content := make([]map[string]interface{}, 0, len(parts))
	for _, part := range parts {
		tr, ok := part.(types.ToolResultContent)
		if !ok {
			msg := fmt.Sprintf("google.interactions: unsupported tool message part type %q; part dropped.", part.ContentType())
			warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
			continue
		}
		block, ws, err := toolResultToInteractionBlock(tr)
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, ws...)
		content = append(content, block)
	}
	return content, warnings, nil
}

func toolResultToInteractionBlock(part types.ToolResultContent) (map[string]interface{}, []types.Warning, error) {
	base := map[string]interface{}{
		"type":    "function_result",
		"call_id": part.ToolCallID,
		"name":    part.ToolName,
	}
	if part.Output == nil {
		if part.Error != "" {
			base["is_error"] = true
			base["result"] = part.Error
		} else {
			base["result"] = fmt.Sprintf("%v", part.Result)
		}
		return base, nil, nil
	}
	switch part.Output.Type {
	case types.ToolResultOutputText:
		base["result"] = fmt.Sprintf("%v", part.Output.Value)
	case types.ToolResultOutputJSON:
		b, _ := json.Marshal(part.Output.Value)
		base["result"] = string(b)
	case types.ToolResultOutputError, types.ToolResultOutputErrorText, types.ToolResultOutputErrorJSON:
		base["is_error"] = true
		base["result"] = fmt.Sprintf("%v", part.Output.Value)
	case types.ToolResultOutputExecutionDenied:
		base["is_error"] = true
		base["result"] = firstNonEmpty(part.Output.Reason, "Tool execution denied by user.")
	case types.ToolResultOutputContent:
		blocks, warnings, err := toolResultContentBlocks(part.Output.Content)
		if err != nil {
			return nil, nil, err
		}
		base["result"] = blocks
		return base, warnings, nil
	default:
		base["result"] = fmt.Sprintf("%v", part.Output.Value)
	}
	return base, nil, nil
}

func toolResultContentBlocks(blocks []types.ToolResultContentBlock) ([]map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	out := make([]map[string]interface{}, 0, len(blocks))
	for _, block := range blocks {
		switch b := block.(type) {
		case types.TextContentBlock:
			out = append(out, map[string]interface{}{"type": "text", "text": b.Text})
		case types.ImageContentBlock:
			out = append(out, map[string]interface{}{"type": "image", "data": base64.StdEncoding.EncodeToString(b.Data), "mime_type": b.MediaType})
		case types.FileContentBlock:
			if !strings.HasPrefix(b.MediaType, "image/") {
				msg := fmt.Sprintf("google.interactions: tool-result file with mediaType %q is not supported (Interactions function_result.result accepts only text and image content); part dropped.", b.MediaType)
				warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
				continue
			}
			file := types.FileContent{FileData: b.FileData, Data: b.Data, MediaType: b.MediaType, URL: b.URL, Reference: b.Reference, Text: b.Text}
			ib, ws, err := fileContentToInteractionBlock(file, "")
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, ws...)
			if ib == nil {
				continue
			}
			out = append(out, ib)
		default:
			msg := fmt.Sprintf("google.interactions: tool-result content part type %q is not supported; part dropped.", block.ToolResultContentType())
			warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
		}
	}
	return out, warnings, nil
}

func (m *InteractionsLanguageModel) parseOutputs(outputs []interactionsContentBlock, interactionID string) ([]types.ContentPart, []types.ToolCall, bool) {
	content := make([]types.ContentPart, 0, len(outputs))
	toolCalls := make([]types.ToolCall, 0)
	hasFunctionCall := false
	for _, block := range outputs {
		switch block.Type {
		case "user_input":
			// skip user input steps in response
		case "model_output":
			var innerBlocks []interactionsContentBlock
			if len(block.ContentRaw) > 0 {
				_ = json.Unmarshal(block.ContentRaw, &innerBlocks)
			}
			for _, inner := range innerBlocks {
				meta := providerMetaRaw("", interactionID)
				switch inner.Type {
				case "text":
					content = append(content, types.TextContent{Text: inner.Text, ProviderMetadata: meta})
					content = append(content, annotationsToSources(inner.Annotations)...)
				case "image":
					if inner.Data != "" {
						data, _ := base64.StdEncoding.DecodeString(inner.Data)
						content = append(content, types.GeneratedFileContent{MediaType: firstNonEmpty(inner.MimeType, "image/png"), Data: data, ProviderMetadata: meta})
					} else if inner.URI != "" {
						mediaType := firstNonEmpty(inner.MimeType, "image/png")
						content = append(content, types.GeneratedFileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeURL, URL: inner.URI, MediaType: mediaType}, URL: inner.URI, ProviderMetadata: meta})
					}
				case "video":
					if inner.Data != "" {
						data, _ := base64.StdEncoding.DecodeString(inner.Data)
						content = append(content, types.GeneratedFileContent{MediaType: firstNonEmpty(inner.MimeType, "video/mp4"), Data: data, ProviderMetadata: meta})
					} else if inner.URI != "" {
						mediaType := firstNonEmpty(inner.MimeType, "video/mp4")
						content = append(content, types.GeneratedFileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeURL, URL: inner.URI, MediaType: mediaType}, URL: inner.URI, ProviderMetadata: meta})
					}
				}
			}
		case "text":
			meta := providerMetaRaw("", interactionID)
			content = append(content, types.TextContent{Text: block.Text, ProviderMetadata: meta})
			content = append(content, annotationsToSources(block.Annotations)...)
		case "thought":
			meta := providerMetaRaw(block.Signature, interactionID)
			content = append(content, types.ReasoningContent{Text: thoughtText(block.Summary), Signature: block.Signature, ProviderMetadata: meta})
		case "image":
			meta := providerMetaRaw("", interactionID)
			if block.Data != "" {
				data, _ := base64.StdEncoding.DecodeString(block.Data)
				content = append(content, types.GeneratedFileContent{MediaType: firstNonEmpty(block.MimeType, "image/png"), Data: data, ProviderMetadata: meta})
			} else if block.URI != "" {
				mediaType := firstNonEmpty(block.MimeType, "image/png")
				content = append(content, types.GeneratedFileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeURL, URL: block.URI, MediaType: mediaType}, URL: block.URI, ProviderMetadata: meta})
			}
		case "processing_call":
			processingID := firstNonEmpty(block.ID, fmt.Sprintf("processing_%d", len(content)+1))
			content = append(content, types.CustomContent{
				Kind:             "google.processing_call",
				ProviderMetadata: processingMeta(block.Signature, interactionID, processingID, ""),
			})
		case "processing_result":
			processingCallID := firstNonEmpty(block.CallID, fmt.Sprintf("processing_%d", len(content)+1))
			content = append(content, types.CustomContent{
				Kind:             "google.processing_result",
				ProviderMetadata: processingMeta(block.Signature, interactionID, "", processingCallID),
			})
		case "function_call":
			hasFunctionCall = true
			var args map[string]interface{}
			if len(block.Arguments) > 0 {
				_ = json.Unmarshal(block.Arguments, &args)
			}
			if args == nil {
				args = map[string]interface{}{}
			}
			id := firstNonEmpty(block.ID, "call_"+fmt.Sprint(len(toolCalls)+1))
			argsJSON, _ := json.Marshal(args)
			metaRaw := providerMetaRaw(block.Signature, interactionID)
			content = append(content, types.ToolCallContent{
				ToolCallID:       id,
				ToolName:         block.Name,
				Input:            string(argsJSON),
				Arguments:        args,
				ProviderMetadata: metaRaw,
				ThoughtSignature: block.Signature,
			})
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               id,
				ToolName:         block.Name,
				Arguments:        args,
				RawArguments:     string(argsJSON),
				ProviderMetadata: providerMetaMap(block.Signature, interactionID),
				ThoughtSignature: block.Signature,
			})
		default:
			if isBuiltinInteractionsToolCall(block.Type) {
				var args map[string]interface{}
				if len(block.Arguments) > 0 {
					_ = json.Unmarshal(block.Arguments, &args)
				}
				if args == nil {
					args = map[string]interface{}{}
				}
				id := firstNonEmpty(block.ID, fmt.Sprintf("call_%d", len(toolCalls)+1))
				argsJSON, _ := json.Marshal(args)
				content = append(content, types.ToolCallContent{
					ToolCallID:       id,
					ToolName:         builtinToolName(block),
					Input:            string(argsJSON),
					Arguments:        args,
					ProviderExecuted: true,
				})
				toolCalls = append(toolCalls, types.ToolCall{
					ID:               id,
					ToolName:         builtinToolName(block),
					Arguments:        args,
					RawArguments:     string(argsJSON),
					ProviderExecuted: true,
					ProviderMetadata: providerMetaMap(block.Signature, interactionID),
				})
			}
			if isBuiltinInteractionsToolResult(block.Type) {
				content = append(content, types.ToolResultContent{
					ToolCallID: block.CallID,
					ToolName:   builtinResultToolName(block),
					Result:     block.Result,
				})
				content = append(content, builtinToolResultSources(block)...)
			}
		}
	}
	return content, toolCalls, hasFunctionCall
}

func compactMessagesForInteraction(messages []types.Message, interactionID string) []types.Message {
	out := make([]types.Message, 0, len(messages))
	droppedToolCalls := map[string]bool{}
	for _, msg := range messages {
		if msg.Role == types.RoleAssistant {
			matches := false
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					matches = matches || googleInteractionID(p.ProviderMetadata) == interactionID
				case types.ReasoningContent:
					matches = matches || googleInteractionID(p.ProviderMetadata) == interactionID
				case types.ToolCallContent:
					matches = matches || googleInteractionID(p.ProviderMetadata) == interactionID
				}
			}
			for _, call := range msg.ToolCalls {
				if googleInteractionFromMap(call.ProviderMetadata) == interactionID {
					matches = true
				}
			}
			if matches {
				for _, call := range msg.ToolCalls {
					droppedToolCalls[call.ID] = true
				}
				continue
			}
		}
		if msg.Role == types.RoleTool {
			filtered := msg
			filtered.Content = nil
			for _, part := range msg.Content {
				if tr, ok := part.(types.ToolResultContent); ok && droppedToolCalls[tr.ToolCallID] {
					continue
				}
				filtered.Content = append(filtered.Content, part)
			}
			if len(filtered.Content) == 0 {
				continue
			}
			out = append(out, filtered)
			continue
		}
		out = append(out, msg)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func interactionMediaKind(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, "image"):
		return "image"
	case strings.HasPrefix(mediaType, "audio"):
		return "audio"
	case strings.HasPrefix(mediaType, "video"):
		return "video"
	case strings.HasPrefix(mediaType, "application"), strings.HasPrefix(mediaType, "text"):
		return "document"
	default:
		return ""
	}
}

func isFullMediaType(mediaType string) bool {
	return strings.Contains(mediaType, "/")
}

func resolveGoogleReference(ref types.ProviderReference) string {
	if ref == nil {
		return ""
	}
	if v := ref["google"]; v != "" {
		return v
	}
	if v := ref[""]; v != "" {
		return v
	}
	for _, v := range ref {
		if v != "" {
			return v
		}
	}
	return ""
}

func reasoningSummary(text string) interface{} {
	if text == "" {
		return nil
	}
	return []map[string]interface{}{{"type": "text", "text": text}}
}

func thoughtText(summary []interactionsContentBlock) string {
	var parts []string
	for _, item := range summary {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func mergeAdjacentInteractionText(content []map[string]interface{}) []map[string]interface{} {
	if len(content) < 2 {
		return content
	}
	out := make([]map[string]interface{}, 0, len(content))
	for _, block := range content {
		last := map[string]interface{}(nil)
		if len(out) > 0 {
			last = out[len(out)-1]
		}
		if block["type"] == "text" && last != nil && last["type"] == "text" {
			last["text"] = fmt.Sprintf("%s\n\n%s", last["text"], block["text"])
			continue
		}
		out = append(out, block)
	}
	return out
}

func signatureFromMetadataMap(meta map[string]interface{}) string {
	if google, ok := meta["google"].(map[string]interface{}); ok {
		if s, ok := google["signature"].(string); ok {
			return s
		}
	}
	return ""
}

func googleInteractionFromMap(meta map[string]interface{}) string {
	if google, ok := meta["google"].(map[string]interface{}); ok {
		if s, ok := google["interactionId"].(string); ok {
			return s
		}
	}
	return ""
}

func isBuiltinInteractionsToolCall(t string) bool {
	switch t {
	case "google_search_call", "code_execution_call", "url_context_call", "file_search_call", "google_maps_call", "mcp_server_tool_call":
		return true
	default:
		return false
	}
}

func isBuiltinInteractionsToolResult(t string) bool {
	switch t {
	case "google_search_result", "code_execution_result", "url_context_result", "file_search_result", "google_maps_result", "mcp_server_tool_result":
		return true
	default:
		return false
	}
}

func builtinToolName(block interactionsContentBlock) string {
	if block.Type == "mcp_server_tool_call" {
		return firstNonEmpty(block.Name, "mcp_server_tool")
	}
	return strings.TrimSuffix(block.Type, "_call")
}

func builtinResultToolName(block interactionsContentBlock) string {
	if block.Type == "mcp_server_tool_result" {
		return firstNonEmpty(block.Name, "mcp_server_tool")
	}
	return strings.TrimSuffix(block.Type, "_result")
}

func annotationsToSources(annotations []interactionsAnnotation) []types.ContentPart {
	out := make([]types.ContentPart, 0)
	for i, ann := range annotations {
		id := fmt.Sprintf("source-%d", i+1)
		switch ann.Type {
		case "url_citation":
			if ann.URL != "" {
				out = append(out, types.SourceContent{SourceType: "url", ID: id, URL: ann.URL, Title: ann.Title})
			}
		case "file_citation":
			uri := firstNonEmpty(ann.DocumentURI, ann.Source, ann.FileName)
			if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
				out = append(out, types.SourceContent{SourceType: "url", ID: id, URL: uri, Title: ann.FileName})
			} else {
				out = append(out, types.SourceContent{SourceType: "document", ID: id, MediaType: inferDocumentMediaType(uri), Title: firstNonEmpty(ann.FileName, baseName(uri), uri), Filename: firstNonEmpty(ann.FileName, baseName(uri))})
			}
		case "place_citation":
			out = append(out, types.SourceContent{SourceType: "url", ID: id, URL: ann.URL, Title: ann.Name})
		}
	}
	return out
}

func builtinToolResultSources(block interactionsContentBlock) []types.ContentPart {
	entries, ok := block.Result.([]interface{})
	if !ok {
		return nil
	}
	out := make([]types.ContentPart, 0)
	for i, raw := range entries {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id := fmt.Sprintf("source-%d", i+1)
		switch block.Type {
		case "url_context_result":
			urlValue, _ := entry["url"].(string)
			status, _ := entry["status"].(string)
			if urlValue != "" && (status == "" || status == "success") {
				out = append(out, types.SourceContent{SourceType: "url", ID: id, URL: urlValue})
			}
		case "google_search_result":
			urlValue, _ := entry["url"].(string)
			title, _ := entry["title"].(string)
			if urlValue != "" {
				out = append(out, types.SourceContent{SourceType: "url", ID: id, URL: urlValue, Title: title})
			}
		case "google_maps_result":
			places, _ := entry["places"].([]interface{})
			for j, rawPlace := range places {
				place, ok := rawPlace.(map[string]interface{})
				if !ok {
					continue
				}
				urlValue, _ := place["url"].(string)
				name, _ := place["name"].(string)
				if urlValue != "" {
					out = append(out, types.SourceContent{SourceType: "url", ID: fmt.Sprintf("%s-%d", id, j+1), URL: urlValue, Title: name})
				}
			}
		case "file_search_result":
			uri, _ := entry["document_uri"].(string)
			if uri == "" {
				uri, _ = entry["source"].(string)
			}
			fileName, _ := entry["file_name"].(string)
			title, _ := entry["title"].(string)
			if uri == "" {
				uri = fileName
			}
			if uri == "" {
				continue
			}
			if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
				out = append(out, types.SourceContent{SourceType: "url", ID: id, URL: uri, Title: title})
			} else {
				out = append(out, types.SourceContent{SourceType: "document", ID: id, MediaType: inferDocumentMediaType(uri), Title: firstNonEmpty(title, fileName, baseName(uri), uri), Filename: firstNonEmpty(fileName, baseName(uri))})
			}
		}
	}
	return out
}

func inferDocumentMediaType(uriOrName string) string {
	lower := strings.ToLower(uriOrName)
	switch {
	case strings.HasSuffix(lower, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(lower, ".txt"):
		return "text/plain"
	case strings.HasSuffix(lower, ".md"), strings.HasSuffix(lower, ".markdown"):
		return "text/markdown"
	case strings.HasSuffix(lower, ".doc"):
		return "application/msword"
	case strings.HasSuffix(lower, ".docx"):
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}

func baseName(uriOrName string) string {
	parts := strings.Split(uriOrName, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
