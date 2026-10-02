package ai

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

func generateResultContentParts(result *types.GenerateResult) []types.ContentPart {
	if result == nil {
		return nil
	}
	parts := append([]types.ContentPart(nil), result.Content...)
	if result.Text != "" && !contentHasText(parts) {
		parts = append([]types.ContentPart{types.TextContent{Text: result.Text}}, parts...)
	}
	seenToolCalls := map[string]bool{}
	for i, part := range parts {
		switch p := part.(type) {
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
		ProviderMetadata: tr.ProviderMetadata,
		ToolMetadata:     tr.ToolMetadata,
		Dynamic:          tr.Dynamic,
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
		return types.ToolErrorContent{
			ToolCallID:       result.ToolCallID,
			ToolName:         result.ToolName,
			Input:            result.Input,
			Error:            result.Error.Error(),
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
