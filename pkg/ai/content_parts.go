package ai

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func appendTextPart(parts []types.ContentPart, text string) []types.ContentPart {
	if text == "" {
		return parts
	}
	if n := len(parts); n > 0 {
		if last, ok := parts[n-1].(types.TextContent); ok {
			last.Text += text
			parts[n-1] = last
			return parts
		}
	}
	return append(parts, types.TextContent{Text: text})
}

func appendReasoningPart(parts []types.ContentPart, text string) []types.ContentPart {
	if text == "" {
		return parts
	}
	if n := len(parts); n > 0 {
		if last, ok := parts[n-1].(types.ReasoningContent); ok {
			last.Text += text
			parts[n-1] = last
			return parts
		}
	}
	return append(parts, types.ReasoningContent{Text: text})
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
	return types.ToolApprovalRequestContent{
		ApprovalID:  approvalIDForToolResult(tr),
		ToolCallID:  tr.ToolCallID,
		ToolCall:    toolCallForToolResult(tr),
		Signature:   tr.ApprovalSignature,
		IsAutomatic: tr.ApprovalStatus == types.ToolApprovalStatusApproved || tr.ApprovalStatus == types.ToolApprovalStatusDenied,
	}
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
			part.Output = &output
			part.Result = nil
		case *types.ToolResultOutput:
			part.Output = output
			part.Result = nil
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

func toolResultModelOutput(result interface{}) *types.ToolResultOutput {
	if text, ok := result.(string); ok {
		return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: text}
	}
	return &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: result}
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
	switch output := result.Result.(type) {
	case types.ToolResultOutput:
		part.Output = &output
		part.Result = nil
	case *types.ToolResultOutput:
		part.Output = output
		part.Result = nil
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
		part.Output = toolResultModelOutput(result.Result)
		part.Result = nil
	}
	return part
}
