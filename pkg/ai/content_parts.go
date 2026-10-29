package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// appendTextPart accumulates a ChunkTypeText delta into stepContent, merging
// consecutive deltas into a single trailing TextContent part. metadata is the
// delta chunk's ProviderMetadata (e.g. Gemini/Gateway thoughtSignature deltas,
// which can arrive with empty Text and non-nil metadata); when non-nil it
// overwrites the accumulated part's ProviderMetadata, matching TS
// stream-text.ts's `activeText.providerMetadata = part.providerMetadata ??
// activeText.providerMetadata` (latest non-nil metadata wins).
func appendTextPart(parts []types.ContentPart, text string, metadata json.RawMessage) []types.ContentPart {
	if text == "" && len(metadata) == 0 {
		return parts
	}
	if n := len(parts); n > 0 {
		if last, ok := parts[n-1].(types.TextContent); ok {
			last.Text += text
			if len(metadata) > 0 {
				last.ProviderMetadata = metadata
			}
			parts[n-1] = last
			return parts
		}
	}
	return append(parts, types.TextContent{Text: text, ProviderMetadata: metadata})
}

// startNewTextPart marks a ChunkTypeTextStart boundary in stepContent,
// unconditionally starting a fresh TextContent part seeded with the
// text-start chunk's ProviderMetadata. This mirrors TS stream-text.ts, which
// keys accumulated text by the text-start chunk's id and always pushes a
// fresh record onto recordedContent for each text-start
// (`activeTextContent[part.id] = {type: 'text', text: emptyString,
// providerMetadata: part.providerMetadata}; recordedContent.push(...)`),
// even before any delta arrives.
//
// This closes two gaps in Go's id-less, delta-driven accumulation
// (appendTextPart, which otherwise only creates a new part lazily on the
// first delta and has no notion of block boundaries):
//
//  1. Two adjacent text-like blocks with no other content between them (for
//     example an Anthropic compaction block immediately followed by a plain
//     text block, each carrying different providerMetadata on its own
//     text-start) would otherwise merge into a single part, with the second
//     block's deltas — and metadata — folded into the first, already-closed
//     block.
//  2. A text-start's own providerMetadata (e.g. Anthropic's
//     {type:"compaction", signature} or citations) would otherwise be lost
//     entirely whenever the immediately following delta chunk carries no
//     metadata of its own, since appendTextPart only reads the *delta's*
//     metadata when it lazily creates the part.
//
// Providers that never emit text-start/text-end at all (openai chat,
// gemini's non-boundary deltas, the openai-compatible providers, ...) are
// unaffected: this function only runs when a text-start chunk is actually
// observed, and appendTextPart's lazy new-part-on-first-delta fallback still
// handles everything else exactly as before.
func startNewTextPart(parts []types.ContentPart, metadata json.RawMessage) []types.ContentPart {
	return append(parts, types.TextContent{ProviderMetadata: metadata})
}

// appendReasoningPart accumulates a ChunkTypeReasoning delta into stepContent,
// merging consecutive deltas into a single trailing ReasoningContent part.
// metadata is the delta chunk's ProviderMetadata (e.g. Anthropic's
// signature_delta, which arrives as its own chunk with empty text and
// non-nil metadata -- the cryptographic signature required to replay a
// thinking block in a later turn); when non-nil it overwrites the
// accumulated part's ProviderMetadata, mirroring appendTextPart's "latest
// non-nil metadata wins" rule (TS stream-text.ts's
// `activeReasoning.providerMetadata = part.providerMetadata ??
// activeReasoning.providerMetadata`).
func appendReasoningPart(parts []types.ContentPart, text string, metadata json.RawMessage) []types.ContentPart {
	if text == "" && len(metadata) == 0 {
		return parts
	}
	if n := len(parts); n > 0 {
		if last, ok := parts[n-1].(types.ReasoningContent); ok {
			last.Text += text
			if len(metadata) > 0 {
				last.ProviderMetadata = metadata
			}
			parts[n-1] = last
			return parts
		}
	}
	return append(parts, types.ReasoningContent{Text: text, ProviderMetadata: metadata})
}

// startNewReasoningPart marks a ChunkTypeReasoningStart boundary in
// stepContent: a fresh ReasoningContent part seeded with the reasoning-start
// chunk's ProviderMetadata (TS stream-text.ts pushes a record per
// reasoning-start, keyed by id). Without it two adjacent reasoning blocks (a
// thinking block followed by a redacted_thinking block, say) would merge into
// one part, and a redacted block's redactedData would be lost.
func startNewReasoningPart(parts []types.ContentPart, metadata json.RawMessage) []types.ContentPart {
	return append(parts, types.ReasoningContent{ProviderMetadata: metadata})
}

// setTrailingReasoningMetadata applies a reasoning-end chunk's ProviderMetadata
// to the trailing reasoning part (TS: activeReasoning.providerMetadata =
// part.providerMetadata ?? activeReasoning.providerMetadata).
func setTrailingReasoningMetadata(parts []types.ContentPart, metadata json.RawMessage) []types.ContentPart {
	if len(metadata) == 0 {
		return parts
	}
	if n := len(parts); n > 0 {
		if last, ok := parts[n-1].(types.ReasoningContent); ok {
			last.ProviderMetadata = metadata
			parts[n-1] = last
		}
	}
	return parts
}

// reasoningPartsOf returns the ReasoningContent parts of content in order.
func reasoningPartsOf(content []types.ContentPart) []types.ReasoningContent {
	var out []types.ReasoningContent
	for _, p := range content {
		if rc, ok := p.(types.ReasoningContent); ok {
			out = append(out, rc)
		}
	}
	return out
}

// reasoningFilesOf returns the reasoning file parts of content (TS
// StepResult.reasoning holds them next to the reasoning parts).
func reasoningFilesOf(content []types.ContentPart) []types.ReasoningFileContent {
	var out []types.ReasoningFileContent
	for _, p := range content {
		switch v := p.(type) {
		case types.ReasoningFileContent:
			out = append(out, v)
		case *types.ReasoningFileContent:
			if v != nil {
				out = append(out, *v)
			}
		}
	}
	return out
}

func generateResultContentParts(result *types.GenerateResult) []types.ContentPart {
	if result == nil {
		return nil
	}
	parts := append([]types.ContentPart(nil), result.Content...)
	if result.Text != "" && !contentHasText(parts) {
		parts = types.InsertLeadText(parts, result.Text)
	}
	seenToolCalls := map[string]bool{}
	for i, part := range parts {
		switch p := part.(type) {
		case types.ToolResultContent:
			// TS convertLanguageModelContent: every tool-result or
			// tool-error the model returns is provider-executed, so it stays
			// in the assistant response message.
			p.ProviderExecuted = true
			parts[i] = p
		case *types.ToolResultContent:
			if p != nil {
				c := *p
				c.ProviderExecuted = true
				parts[i] = c
			}
		case types.ToolErrorContent:
			p.ProviderExecuted = true
			parts[i] = p
		case *types.ToolErrorContent:
			if p != nil {
				c := *p
				c.ProviderExecuted = true
				parts[i] = c
			}
		case types.ToolCallContent:
			seenToolCalls[p.ToolCallID] = true
			if call, ok := toolCallByID(result.ToolCalls, p.ToolCallID); ok {
				parts[i] = contentPartFromToolCall(call)
			}
		case *types.ToolCallContent:
			if p != nil {
				seenToolCalls[p.ToolCallID] = true
				if call, ok := toolCallByID(result.ToolCalls, p.ToolCallID); ok {
					parts[i] = contentPartFromToolCall(call)
				}
			}
		}
	}
	for _, call := range result.ToolCalls {
		if seenToolCalls[call.ID] {
			continue
		}
		parts = append(parts, contentPartFromToolCall(call))
	}
	parts = replaceToolResultContentParts(parts, result.ToolCalls)
	return parts
}

func replaceToolCallContentParts(parts []types.ContentPart, calls []types.ToolCall) []types.ContentPart {
	if len(parts) == 0 || len(calls) == 0 {
		return parts
	}
	for i, part := range parts {
		switch p := part.(type) {
		case types.ToolCallContent:
			if call, ok := toolCallByID(calls, p.ToolCallID); ok {
				parts[i] = contentPartFromToolCall(call)
			}
		case *types.ToolCallContent:
			if p != nil {
				if call, ok := toolCallByID(calls, p.ToolCallID); ok {
					parts[i] = contentPartFromToolCall(call)
				}
			}
		}
	}
	return parts
}

func replaceToolResultContentParts(parts []types.ContentPart, calls []types.ToolCall) []types.ContentPart {
	if len(parts) == 0 || len(calls) == 0 {
		return parts
	}
	for i, part := range parts {
		switch p := part.(type) {
		case types.ToolResultContent:
			if call, ok := toolCallByID(calls, p.ToolCallID); ok {
				parts[i] = enrichToolResultContent(p, call)
			}
		case *types.ToolResultContent:
			if p != nil {
				if call, ok := toolCallByID(calls, p.ToolCallID); ok {
					parts[i] = enrichToolResultContent(*p, call)
				}
			}
		case types.ToolErrorContent:
			if call, ok := toolCallByID(calls, p.ToolCallID); ok {
				parts[i] = enrichToolErrorContent(p, call)
			}
		case *types.ToolErrorContent:
			if p != nil {
				if call, ok := toolCallByID(calls, p.ToolCallID); ok {
					parts[i] = enrichToolErrorContent(*p, call)
				}
			}
		}
	}
	return parts
}

func enrichToolResultContent(part types.ToolResultContent, call types.ToolCall) types.ToolResultContent {
	if part.Input == nil {
		part.Input = call.Arguments
	}
	if part.ToolMetadata == nil {
		part.ToolMetadata = call.ToolMetadata
	}
	part.Dynamic = call.Dynamic
	return part
}

func enrichToolErrorContent(part types.ToolErrorContent, call types.ToolCall) types.ToolErrorContent {
	if part.Input == nil {
		part.Input = call.Arguments
	}
	if part.ToolMetadata == nil {
		part.ToolMetadata = call.ToolMetadata
	}
	part.Dynamic = call.Dynamic
	return part
}

func toolCallByID(calls []types.ToolCall, id string) (types.ToolCall, bool) {
	for _, call := range calls {
		if call.ID == id {
			return call, true
		}
	}
	return types.ToolCall{}, false
}

func contentPartFromToolCall(call types.ToolCall) types.ToolCallContent {
	return types.ToolCallContent{
		ToolCallID:       call.ID,
		ToolName:         call.ToolName,
		Title:            call.Title,
		Input:            call.RawArguments,
		Arguments:        call.Arguments,
		ProviderExecuted: call.ProviderExecuted,
		ProviderMetadata: providerMetadataRaw(call.ProviderMetadata),
		ToolMetadata:     call.ToolMetadata,
		Dynamic:          call.Dynamic,
		Invalid:          call.Invalid,
		Error:            toolCallContentError(call.Error),
		ThoughtSignature: call.ThoughtSignature,
	}
}

func toolCallContentError(err error) interface{} {
	if err == nil {
		return nil
	}
	return err.Error()
}

func providerMetadataRaw(metadata map[string]interface{}) json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil
	}
	return raw
}

// decodeProviderMetadataMap is the inverse of providerMetadataRaw: it decodes
// a chunk's raw provider metadata back into a map for structured events such
// as LanguageModelCallEndEvent.
func decodeProviderMetadataMap(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func contentHasText(parts []types.ContentPart) bool {
	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			if p.Text != "" {
				return true
			}
		case *types.TextContent:
			if p != nil && p.Text != "" {
				return true
			}
		}
	}
	return false
}

func attachToolApprovalSignatures(results []types.ToolResult, secret []byte) {
	if secret == nil {
		return
	}
	for i := range results {
		switch results[i].ApprovalStatus {
		case types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied:
		default:
			continue
		}
		if results[i].ApprovalSignature != "" {
			continue
		}
		approvalID := results[i].ApprovalID
		if approvalID == "" {
			approvalID = results[i].ToolCallID
		}
		signature, err := SignToolApproval(secret, approvalID, results[i].ToolCallID, results[i].ToolName, results[i].Input)
		if err == nil {
			results[i].ApprovalSignature = signature
		}
	}
}

func approvalIDForToolResult(tr types.ToolResult) string {
	if tr.ApprovalID != "" {
		return tr.ApprovalID
	}
	return tr.ToolCallID
}

func toolCallForToolResult(tr types.ToolResult) types.ToolCall {
	return types.ToolCall{
		ID:               tr.ToolCallID,
		ToolName:         tr.ToolName,
		Title:            tr.Title,
		Arguments:        tr.Input,
		ProviderExecuted: tr.ProviderExecuted,
		// TS keeps the parsed tool call's providerExecuted, explicit false
		// included.
		ProviderExecutedSet: tr.ProviderExecutedSet,
		ProviderMetadata:    tr.ProviderMetadata,
		ToolMetadata:        tr.ToolMetadata,
		Dynamic:             tr.Dynamic,
	}
}

func toolApprovalRequestFromToolResult(tr types.ToolResult) types.ToolApprovalRequestContent {
	request := types.ToolApprovalRequestContent{
		ApprovalID:  approvalIDForToolResult(tr),
		ToolCallID:  tr.ToolCallID,
		ToolCall:    toolCallForToolResult(tr),
		Signature:   tr.ApprovalSignature,
		IsAutomatic: tr.ApprovalStatus == types.ToolApprovalStatusApproved || tr.ApprovalStatus == types.ToolApprovalStatusDenied,
	}
	// user-approval reasons are shown on the request; approved/denied reasons
	// are emitted on the response.
	if tr.ApprovalStatus == types.ToolApprovalStatusUserApproval && tr.ApprovalReason != nil {
		request.Reason = *tr.ApprovalReason
	}
	return request
}

func toolApprovalResponseFromToolResult(tr types.ToolResult) types.ToolApprovalResponseContent {
	reason := ""
	if tr.ApprovalReason != nil {
		reason = *tr.ApprovalReason
	}
	return types.ToolApprovalResponseContent{
		ApprovalID:       approvalIDForToolResult(tr),
		ToolCallID:       tr.ToolCallID,
		ToolCall:         toolCallForToolResult(tr),
		Approved:         tr.ApprovalStatus == types.ToolApprovalStatusApproved,
		Reason:           reason,
		ProviderExecuted: tr.ProviderExecuted,
		// TS: providerExecuted: toolCall.providerExecuted, so an explicit
		// false on the call reaches the response.
		ProviderExecutedSet: tr.ProviderExecutedSet,
	}
}

func toolResultsToContentParts(results []types.ToolResult, secret ...[]byte) []types.ContentPart {
	if len(results) == 0 {
		return nil
	}
	if len(secret) > 0 && secret[0] != nil {
		attachToolApprovalSignatures(results, secret[0])
	}
	toolOutputsWithoutApproval := make([]types.ContentPart, 0, len(results))
	approvalRequests := make([]types.ContentPart, 0)
	approvalResponses := make([]types.ContentPart, 0)
	toolOutputsWithApproval := make([]types.ContentPart, 0)
	for _, tr := range results {
		hasApproval := tr.ApprovalStatus == types.ToolApprovalStatusUserApproval ||
			tr.ApprovalStatus == types.ToolApprovalStatusApproved ||
			tr.ApprovalStatus == types.ToolApprovalStatusDenied
		if tr.ProviderExecuted && !hasApproval {
			continue
		}
		approvalRequest := toolApprovalRequestFromToolResult(tr)
		switch tr.ApprovalStatus {
		case types.ToolApprovalStatusUserApproval:
			approvalRequests = append(approvalRequests, approvalRequest)
			continue
		case types.ToolApprovalStatusApproved:
			approvalRequests = append(approvalRequests, approvalRequest)
			approvalResponses = append(approvalResponses, toolApprovalResponseFromToolResult(tr))
			if tr.ProviderExecuted {
				continue
			}
		case types.ToolApprovalStatusDenied:
			approvalRequests = append(approvalRequests, approvalRequest)
			approvalResponses = append(approvalResponses, toolApprovalResponseFromToolResult(tr))
			continue
		}
		part := types.ToolResultContent{
			ToolCallID:       tr.ToolCallID,
			ToolName:         tr.ToolName,
			Input:            tr.Input,
			Result:           tr.Result,
			IsError:          tr.IsError,
			ProviderExecuted: tr.ProviderExecuted,
			ProviderMetadata: providerMetadataRaw(tr.ProviderMetadata),
			ToolMetadata:     tr.ToolMetadata,
			Dynamic:          tr.Dynamic,
			Preliminary:      tr.Preliminary,
		}
		if tr.ModelOutput != nil {
			part.Output = tr.ModelOutput
			part.Result = nil
		}
		if tr.Error != nil {
			errPart := types.ToolErrorContent{
				ToolCallID:       tr.ToolCallID,
				ToolName:         tr.ToolName,
				Input:            tr.Input,
				Error:            tr.Error.Error(),
				ErrorText:        providerutils.ToolErrorText(tr.Error),
				ProviderExecuted: tr.ProviderExecuted,
				ProviderMetadata: providerMetadataRaw(tr.ProviderMetadata),
				ToolMetadata:     tr.ToolMetadata,
				Dynamic:          tr.Dynamic,
			}
			if tr.ApprovalStatus == types.ToolApprovalStatusApproved {
				toolOutputsWithApproval = append(toolOutputsWithApproval, errPart)
			} else {
				toolOutputsWithoutApproval = append(toolOutputsWithoutApproval, errPart)
			}
			continue
		}
		switch output := tr.Result.(type) {
		case types.ToolResultOutput:
			if part.Output == nil {
				part.Output = &output
				part.Result = nil
			}
		case *types.ToolResultOutput:
			if part.Output == nil {
				part.Output = output
				part.Result = nil
			}
		case error:
			errPart := types.ToolErrorContent{
				ToolCallID:       tr.ToolCallID,
				ToolName:         tr.ToolName,
				Input:            tr.Input,
				Error:            output.Error(),
				ErrorText:        providerutils.ToolErrorText(output),
				ProviderExecuted: tr.ProviderExecuted,
				ProviderMetadata: providerMetadataRaw(tr.ProviderMetadata),
				ToolMetadata:     tr.ToolMetadata,
				Dynamic:          tr.Dynamic,
			}
			if tr.ApprovalStatus == types.ToolApprovalStatusApproved {
				toolOutputsWithApproval = append(toolOutputsWithApproval, errPart)
			} else {
				toolOutputsWithoutApproval = append(toolOutputsWithoutApproval, errPart)
			}
			continue
		case nil:
			if part.Output == nil && tr.Error == nil {
				part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: nil}
				part.Result = nil
			}
		default:
			if part.Output == nil {
				part.Output = toolResultModelOutput(tr.Result)
				part.Result = nil
			}
		}
		if tr.ApprovalStatus == types.ToolApprovalStatusApproved || tr.ApprovalStatus == types.ToolApprovalStatusDenied {
			toolOutputsWithApproval = append(toolOutputsWithApproval, part)
		} else {
			toolOutputsWithoutApproval = append(toolOutputsWithoutApproval, part)
		}
	}
	parts := make([]types.ContentPart, 0, len(toolOutputsWithoutApproval)+len(approvalRequests)+len(approvalResponses)+len(toolOutputsWithApproval))
	parts = append(parts, toolOutputsWithoutApproval...)
	parts = append(parts, approvalRequests...)
	parts = append(parts, approvalResponses...)
	parts = append(parts, toolOutputsWithApproval...)
	return parts
}

// toolResultModelOutput mirrors TS createToolModelOutput's default case:
// string results pass through as "text"; everything else is normalized to a
// plain JSON value via toJSONValue (round-tripped through JSON so structs,
// time.Time, etc. match what the message actually serializes to) before
// being stored as "json" (audit row 6aa7c54 / WG24).
func toolResultModelOutput(result interface{}) *types.ToolResultOutput {
	if text, ok := result.(string); ok {
		return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: text}
	}
	return &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: toJSONValue(result)}
}

func toolResultContentFromToolResult(result types.ToolResult) types.ContentPart {
	if result.Error != nil {
		// A provider-executed error is the provider's own error value (TS
		// tool-error.error: a string or a structured object such as
		// {type: 'web_fetch_tool_result_error', errorCode}), replayed as
		// error-json. Providers that carry the structured payload put it in
		// Result next to the Error summary.
		var errValue interface{} = result.Error.Error()
		errorText := providerutils.ToolErrorText(result.Error)
		if result.ProviderExecuted {
			errorText = ""
			if result.Result != nil {
				errValue = result.Result
			}
		}
		return types.ToolErrorContent{
			ToolCallID:       result.ToolCallID,
			ToolName:         result.ToolName,
			Input:            result.Input,
			Error:            errValue,
			ErrorText:        errorText,
			ProviderExecuted: result.ProviderExecuted,
			ProviderMetadata: providerMetadataRaw(result.ProviderMetadata),
			ToolMetadata:     result.ToolMetadata,
			Dynamic:          result.Dynamic,
		}
	}
	part := types.ToolResultContent{
		ToolCallID:       result.ToolCallID,
		ToolName:         result.ToolName,
		Input:            result.Input,
		Result:           result.Result,
		IsError:          result.IsError,
		ProviderExecuted: result.ProviderExecuted,
		ProviderMetadata: providerMetadataRaw(result.ProviderMetadata),
		ToolMetadata:     result.ToolMetadata,
		Dynamic:          result.Dynamic,
		Preliminary:      result.Preliminary,
	}
	if result.ModelOutput != nil {
		part.Output = result.ModelOutput
		part.Result = nil
	}
	switch output := result.Result.(type) {
	case types.ToolResultOutput:
		if part.Output == nil {
			part.Output = &output
			part.Result = nil
		}
	case *types.ToolResultOutput:
		if part.Output == nil {
			part.Output = output
			part.Result = nil
		}
	case error:
		return types.ToolErrorContent{
			ToolCallID:       result.ToolCallID,
			ToolName:         result.ToolName,
			Input:            result.Input,
			Error:            output.Error(),
			ErrorText:        providerutils.ToolErrorText(output),
			ProviderExecuted: result.ProviderExecuted,
			ProviderMetadata: providerMetadataRaw(result.ProviderMetadata),
			ToolMetadata:     result.ToolMetadata,
			Dynamic:          result.Dynamic,
		}
	default:
		if part.Output == nil {
			part.Output = toolResultModelOutput(result.Result)
			part.Result = nil
		}
	}
	return part
}

// setTrailingTextEnd applies a text-end chunk to the most recent TextContent
// part in parts (TS stream-text.ts: `activeText.providerMetadata =
// part.providerMetadata ?? activeText.providerMetadata; if (part.citations !=
// null) activeText.citations = part.citations`).
func setTrailingTextEnd(parts []types.ContentPart, metadata json.RawMessage, citations []types.Citation) []types.ContentPart {
	if len(citations) == 0 && len(metadata) == 0 {
		return parts
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if text, ok := parts[i].(types.TextContent); ok {
			if len(metadata) > 0 {
				text.ProviderMetadata = metadata
			}
			if len(citations) > 0 {
				text.Citations = citations
			}
			parts[i] = text
			return parts
		}
	}
	return parts
}

// normalizeGenerateResultText fills GenerateResult.Text from the text parts of
// Content when a provider returned its text only there. TS derives text from
// the text parts of content (extractTextContent), so core accepts both shapes:
// text in Text with the remaining parts in Content, or every part in Content.
func normalizeGenerateResultText(result *types.GenerateResult) {
	if result == nil || result.Text != "" {
		return
	}
	var text strings.Builder
	for _, part := range result.Content {
		switch p := part.(type) {
		case types.TextContent:
			text.WriteString(p.Text)
		case *types.TextContent:
			if p != nil {
				text.WriteString(p.Text)
			}
		}
	}
	result.Text = text.String()
}

// providerToolResultsFromContent converts the tool-result and tool-error parts
// a model response carries (results the provider executed) into ToolResults,
// skipping calls that already have an executed result in executed. TS
// `toolResults` holds these parts alongside the locally executed results.
func providerToolResultsFromContent(content []types.ContentPart, executed []types.ToolResult) []types.ToolResult {
	var out []types.ToolResult
	seen := make(map[string]bool, len(executed))
	for _, tr := range executed {
		if !tr.ProviderExecuted || tr.Result != nil || tr.Error != nil {
			seen[tr.ToolCallID] = true
		}
	}
	for _, part := range content {
		var tr types.ToolResult
		switch p := part.(type) {
		case types.ToolResultContent:
			tr = providerToolResultFromResult(p)
		case *types.ToolResultContent:
			if p == nil {
				continue
			}
			tr = providerToolResultFromResult(*p)
		case types.ToolErrorContent:
			tr = providerToolResultFromError(p)
		case *types.ToolErrorContent:
			if p == nil {
				continue
			}
			tr = providerToolResultFromError(*p)
		default:
			continue
		}
		if seen[tr.ToolCallID] {
			continue
		}
		seen[tr.ToolCallID] = true
		out = append(out, tr)
	}
	return out
}

func providerToolResultFromResult(p types.ToolResultContent) types.ToolResult {
	tr := types.ToolResult{
		ToolCallID:       p.ToolCallID,
		ToolName:         p.ToolName,
		Title:            p.Title,
		Input:            p.Input,
		Result:           p.Result,
		IsError:          p.IsError,
		ProviderExecuted: true,
		ProviderMetadata: decodeProviderMetadataMap(p.ProviderMetadata),
		ToolMetadata:     p.ToolMetadata,
		Dynamic:          p.Dynamic,
		Preliminary:      p.Preliminary,
	}
	if p.Error != "" {
		tr.Error = errors.New(p.Error)
	}
	if tr.Result == nil && p.Output != nil {
		tr.Result = p.Output.Value
		tr.ModelOutput = p.Output
	}
	return tr
}

func providerToolResultFromError(p types.ToolErrorContent) types.ToolResult {
	var err error
	var structured interface{}
	switch e := p.Error.(type) {
	case error:
		err = e
	case string:
		err = errors.New(e)
	default:
		// A structured provider error (TS tool-error.error as an object)
		// keeps its value in Result next to the Error summary, the shape
		// toolResultContentFromToolResult replays as error-json.
		err = fmt.Errorf("%v", e)
		structured = e
	}
	return types.ToolResult{
		Result:           structured,
		ToolCallID:       p.ToolCallID,
		ToolName:         p.ToolName,
		Title:            p.Title,
		Input:            p.Input,
		Error:            err,
		ProviderExecuted: true,
		ProviderMetadata: decodeProviderMetadataMap(p.ProviderMetadata),
		ToolMetadata:     p.ToolMetadata,
		Dynamic:          p.Dynamic,
	}
}

// blockContent tracks the text and reasoning blocks a model stream opened with
// an explicit start part, by block id. TS stream-text records content per id
// (activeText / activeReasoning), so blocks the provider interleaves (a
// second text block starting before the first ended) keep their own deltas.
// A delta whose id has no open block (providers without start parts) falls
// back to merging into the trailing part, as before.
type blockContent struct {
	text      map[string]int
	reasoning map[string]int
}

func (b *blockContent) reset() { b.text, b.reasoning = nil, nil }

func (b *blockContent) openText(parts []types.ContentPart, id string, metadata json.RawMessage) []types.ContentPart {
	parts = startNewTextPart(parts, metadata)
	if id != "" {
		if b.text == nil {
			b.text = map[string]int{}
		}
		b.text[id] = len(parts) - 1
	}
	return parts
}

func (b *blockContent) textDelta(parts []types.ContentPart, id, text string, metadata json.RawMessage) []types.ContentPart {
	if i, ok := b.text[id]; ok && id != "" {
		if text == "" && len(metadata) == 0 {
			return parts
		}
		if tc, ok := parts[i].(types.TextContent); ok {
			tc.Text += text
			if len(metadata) > 0 {
				tc.ProviderMetadata = metadata
			}
			parts[i] = tc
			return parts
		}
	}
	return appendTextPart(parts, text, metadata)
}

func (b *blockContent) closeText(parts []types.ContentPart, id string, metadata json.RawMessage, citations []types.Citation) []types.ContentPart {
	if i, ok := b.text[id]; ok && id != "" {
		delete(b.text, id)
		if tc, ok := parts[i].(types.TextContent); ok {
			if len(metadata) > 0 {
				tc.ProviderMetadata = metadata
			}
			if len(citations) > 0 {
				tc.Citations = citations
			}
			parts[i] = tc
		}
		return parts
	}
	return setTrailingTextEnd(parts, metadata, citations)
}

func (b *blockContent) openReasoning(parts []types.ContentPart, id string, metadata json.RawMessage) []types.ContentPart {
	parts = startNewReasoningPart(parts, metadata)
	if id != "" {
		if b.reasoning == nil {
			b.reasoning = map[string]int{}
		}
		b.reasoning[id] = len(parts) - 1
	}
	return parts
}

func (b *blockContent) reasoningDelta(parts []types.ContentPart, id, text string, metadata json.RawMessage) []types.ContentPart {
	if i, ok := b.reasoning[id]; ok && id != "" {
		if text == "" && len(metadata) == 0 {
			return parts
		}
		if rc, ok := parts[i].(types.ReasoningContent); ok {
			rc.Text += text
			if len(metadata) > 0 {
				rc.ProviderMetadata = metadata
			}
			parts[i] = rc
			return parts
		}
	}
	return appendReasoningPart(parts, text, metadata)
}

func (b *blockContent) closeReasoning(parts []types.ContentPart, id string, metadata json.RawMessage) []types.ContentPart {
	if i, ok := b.reasoning[id]; ok && id != "" {
		delete(b.reasoning, id)
		if len(metadata) > 0 {
			if rc, ok := parts[i].(types.ReasoningContent); ok {
				rc.ProviderMetadata = metadata
				parts[i] = rc
			}
		}
		return parts
	}
	return setTrailingReasoningMetadata(parts, metadata)
}

// textOfContent joins the text parts of content in order (TS extractTextContent).
func textOfContent(parts []types.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
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

// validateToolResultParts mirrors TS convertLanguageModelContent: a
// tool-result (or tool-error) part must answer a tool call of the same
// response, unless it is the deferred result of a provider tool that supports
// deferred results. Anything else throws "Tool call <id> not found.".
func validateToolResultParts(content []types.ContentPart, calls []types.ToolCall, tools map[string]*types.Tool) error {
	for _, part := range content {
		var id, name string
		switch p := part.(type) {
		case types.ToolResultContent:
			id, name = p.ToolCallID, p.ToolName
		case *types.ToolResultContent:
			if p == nil {
				continue
			}
			id, name = p.ToolCallID, p.ToolName
		case types.ToolErrorContent:
			id, name = p.ToolCallID, p.ToolName
		case *types.ToolErrorContent:
			if p == nil {
				continue
			}
			id, name = p.ToolCallID, p.ToolName
		default:
			continue
		}
		if _, ok := toolCallByID(calls, id); ok {
			continue
		}
		if t := tools[name]; t != nil && t.ProviderExecuted && t.SupportsDeferredResults {
			continue
		}
		return fmt.Errorf("Tool call %s not found.", id) //nolint:staticcheck // matches the TS SDK's exact error text
	}
	return nil
}

// ValidateToolResultParts runs the "Tool call <id> not found." check of TS
// convertLanguageModelContent for callers outside this package (the legacy
// agent loop). tools is the step's tool set.
func ValidateToolResultParts(content []types.ContentPart, calls []types.ToolCall, tools []types.Tool) error {
	byName := make(map[string]*types.Tool, len(tools))
	for i := range tools {
		byName[tools[i].Name] = &tools[i]
	}
	return validateToolResultParts(content, calls, byName)
}
