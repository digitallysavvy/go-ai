package providerutils

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertToResponseMessage converts accumulated assistant content and tool
// calls into a response message suitable for adding to conversation history.
//
// It mirrors the TypeScript SDK's response-message conversion behavior for
// tool calls: invalid raw JSON inputs are sanitized to an empty object, and nil
// tool-call arguments default to an empty JSON object.
func ConvertToResponseMessage(toolCalls []types.ToolCall, content []types.ContentPart) types.Message {
	msg := types.Message{
		Role:    types.RoleAssistant,
		Content: filterResponseContent(content),
	}
	if len(toolCalls) == 0 {
		return msg
	}
	msg.ToolCalls = make([]types.ToolCall, 0, len(toolCalls))
	for _, call := range toolCalls {
		msg.ToolCalls = append(msg.ToolCalls, normalizeResponseToolCall(call))
	}
	return msg
}

// ConvertToResponseMessages returns the assistant response message followed by
// a single tool message when local tool results or approval responses are present.
func ConvertToResponseMessages(toolCalls []types.ToolCall, content []types.ContentPart, toolResults []types.ToolResult) []types.Message {
	messages := make([]types.Message, 0, 2)
	assistantMessage := ConvertToResponseMessage(toolCalls, content)
	if responseMessageHasContent(assistantMessage) {
		messages = append(messages, assistantMessage)
	}
	parts := make([]types.ContentPart, 0, len(toolResults))
	seenToolResultIDs := map[string]bool{}
	for _, part := range content {
		switch p := part.(type) {
		case types.ToolApprovalResponseContent:
			parts = append(parts, responseToolApprovalResponseContent(p))
			if !p.Approved {
				denied := deniedApprovalToolResultContent(p)
				seenToolResultIDs[denied.ToolCallID] = true
				parts = append(parts, denied)
			}
		case *types.ToolApprovalResponseContent:
			if p != nil {
				parts = append(parts, responseToolApprovalResponseContent(*p))
				if !p.Approved {
					denied := deniedApprovalToolResultContent(*p)
					seenToolResultIDs[denied.ToolCallID] = true
					parts = append(parts, denied)
				}
			}
		case types.ToolResultContent:
			if !p.ProviderExecuted {
				seenToolResultIDs[p.ToolCallID] = true
				parts = append(parts, normalizeToolResultContent(p))
			}
		case *types.ToolResultContent:
			if p != nil && !p.ProviderExecuted {
				seenToolResultIDs[p.ToolCallID] = true
				parts = append(parts, normalizeToolResultContent(*p))
			}
		case types.ToolErrorContent:
			if !p.ProviderExecuted {
				seenToolResultIDs[p.ToolCallID] = true
				parts = append(parts, normalizeToolErrorContent(p, false))
			}
		case *types.ToolErrorContent:
			if p != nil && !p.ProviderExecuted {
				seenToolResultIDs[p.ToolCallID] = true
				parts = append(parts, normalizeToolErrorContent(*p, false))
			}
		}
	}
	if len(toolResults) > 0 {
		for _, result := range toolResults {
			if result.ProviderExecuted || seenToolResultIDs[result.ToolCallID] {
				continue
			}
			seenToolResultIDs[result.ToolCallID] = true
			parts = append(parts, toolResultContentFromResult(result))
		}
	}
	if len(parts) > 0 {
		messages = append(messages, types.Message{
			Role:    types.RoleTool,
			Content: parts,
		})
	}
	return messages
}

func approvalResponseToolCallID(response types.ToolApprovalResponseContent) string {
	if response.ToolCall.ID != "" {
		return response.ToolCall.ID
	}
	return response.ToolCallID
}

func deniedApprovalToolResultContent(response types.ToolApprovalResponseContent) types.ToolResultContent {
	reason := response.Reason
	return types.ToolResultContent{
		ToolCallID: approvalResponseToolCallID(response),
		ToolName:   response.ToolCall.ToolName,
		Output: &types.ToolResultOutput{
			Type:   types.ToolResultOutputExecutionDenied,
			Reason: reason,
		},
	}
}

func toolResultContentFromResult(result types.ToolResult) types.ToolResultContent {
	part := types.ToolResultContent{
		ToolCallID:       result.ToolCallID,
		ToolName:         result.ToolName,
		Title:            result.Title,
		Input:            result.Input,
		Result:           result.Result,
		ProviderExecuted: result.ProviderExecuted,
		ProviderMetadata: providerMetadataRaw(result.ProviderMetadata),
		ToolMetadata:     result.ToolMetadata,
		Dynamic:          result.Dynamic,
		Preliminary:      result.Preliminary,
	}
	if result.Error != nil {
		return normalizeToolErrorContent(types.ToolErrorContent{
			ToolCallID:       result.ToolCallID,
			ToolName:         result.ToolName,
			Title:            result.Title,
			Input:            result.Input,
			Error:            result.Error.Error(),
			ProviderExecuted: result.ProviderExecuted,
			ProviderMetadata: providerMetadataRaw(result.ProviderMetadata),
			ToolMetadata:     result.ToolMetadata,
			Dynamic:          result.Dynamic,
		}, false)
	}
	switch output := result.Result.(type) {
	case types.ToolResultOutput:
		part.Output = &output
		part.Result = nil
	case *types.ToolResultOutput:
		part.Output = output
		part.Result = nil
	case error:
		return normalizeToolErrorContent(types.ToolErrorContent{
			ToolCallID:       result.ToolCallID,
			ToolName:         result.ToolName,
			Title:            result.Title,
			Input:            result.Input,
			Error:            output.Error(),
			ProviderExecuted: result.ProviderExecuted,
			ProviderMetadata: providerMetadataRaw(result.ProviderMetadata),
			ToolMetadata:     result.ToolMetadata,
			Dynamic:          result.Dynamic,
		}, result.ProviderExecuted)
	case string:
		part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: output}
		part.Result = nil
	case nil:
		if part.Output == nil && result.Error == nil {
			part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: nil}
			part.Result = nil
		}
	default:
		if part.Output == nil {
			part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: result.Result}
			part.Result = nil
		}
	}
	return normalizeToolResultContent(part)
}

func normalizeToolResultContent(part types.ToolResultContent) types.ToolResultContent {
	part.ProviderOptions = providerMetadataOptions(part.ProviderMetadata)
	part.ProviderMetadata = nil
	part.Title = ""
	part.Input = nil
	part.ProviderExecuted = false
	part.ToolMetadata = nil
	part.Dynamic = false
	part.Preliminary = false
	if part.Output != nil {
		part.Result = nil
		return part
	}
	switch result := part.Result.(type) {
	case types.ToolResultOutput:
		part.Output = &result
		part.Result = nil
	case *types.ToolResultOutput:
		part.Output = result
		part.Result = nil
	case error:
		part.Error = result.Error()
		part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: result.Error()}
		part.Result = nil
	case string:
		part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: result}
		part.Result = nil
	default:
		part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: result}
		part.Result = nil
	}
	return part
}

func normalizeToolErrorContent(part types.ToolErrorContent, providerExecuted bool) types.ToolResultContent {
	outputType := types.ToolResultOutputErrorText
	if providerExecuted {
		outputType = types.ToolResultOutputErrorJSON
	}
	return types.ToolResultContent{
		ToolCallID:      part.ToolCallID,
		ToolName:        part.ToolName,
		Output:          &types.ToolResultOutput{Type: outputType, Value: part.Error},
		ProviderOptions: providerMetadataOptions(part.ProviderMetadata),
	}
}

func responseMessageHasContent(msg types.Message) bool {
	return len(msg.Content) > 0 || len(msg.ToolCalls) > 0
}

func normalizeResponseToolCall(call types.ToolCall) types.ToolCall {
	if call.RawArguments != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(call.RawArguments), &parsed); err != nil {
			call.Arguments = map[string]interface{}{}
			return stripResponseToolCallMetadata(call)
		}
		if parsed == nil {
			parsed = map[string]interface{}{}
		}
		call.Arguments = parsed
	}
	if call.Arguments == nil {
		call.Arguments = map[string]interface{}{}
	}
	return stripResponseToolCallMetadata(call)
}

func stripResponseToolCallMetadata(call types.ToolCall) types.ToolCall {
	call.Title = ""
	call.ToolMetadata = nil
	call.Dynamic = false
	call.Invalid = false
	call.Error = nil
	return call
}

func filterResponseContent(content []types.ContentPart) []types.ContentPart {
	if len(content) == 0 {
		return nil
	}
	filtered := make([]types.ContentPart, 0, len(content))
	for _, part := range content {
		switch p := part.(type) {
		case types.TextContent:
			if p.Text == "" {
				continue
			}
			filtered = append(filtered, responseTextContent(p))
			continue
		case *types.TextContent:
			if p == nil || p.Text == "" {
				continue
			}
			filtered = append(filtered, responseTextContent(*p))
			continue
		case types.ReasoningContent:
			filtered = append(filtered, responseReasoningContent(p))
			continue
		case *types.ReasoningContent:
			if p == nil {
				continue
			}
			filtered = append(filtered, responseReasoningContent(*p))
			continue
		case types.FileContent:
			filtered = append(filtered, responseFileContent(p))
			continue
		case *types.FileContent:
			if p == nil {
				continue
			}
			filtered = append(filtered, responseFileContent(*p))
			continue
		case types.GeneratedFileContent:
			filtered = append(filtered, responseGeneratedFileContent(p))
			continue
		case *types.GeneratedFileContent:
			if p == nil {
				continue
			}
			filtered = append(filtered, responseGeneratedFileContent(*p))
			continue
		case types.CustomContent:
			filtered = append(filtered, responseCustomContent(p))
			continue
		case *types.CustomContent:
			if p == nil {
				continue
			}
			filtered = append(filtered, responseCustomContent(*p))
			continue
		case types.ReasoningFileContent:
			filtered = append(filtered, responseReasoningFileContent(p))
			continue
		case *types.ReasoningFileContent:
			if p == nil {
				continue
			}
			filtered = append(filtered, responseReasoningFileContent(*p))
			continue
		case types.ToolCallContent:
			filtered = append(filtered, responseToolCallContent(p))
			continue
		case *types.ToolCallContent:
			if p == nil {
				continue
			}
			filtered = append(filtered, responseToolCallContent(*p))
			continue
		case types.ToolApprovalRequestContent:
			filtered = append(filtered, responseToolApprovalRequestContent(p))
			continue
		case *types.ToolApprovalRequestContent:
			if p == nil {
				continue
			}
			filtered = append(filtered, responseToolApprovalRequestContent(*p))
			continue
		case types.SourceContent, *types.SourceContent:
			continue
		case types.ToolResultContent:
			if !p.ProviderExecuted {
				continue
			}
			filtered = append(filtered, normalizeToolResultContent(p))
			continue
		case *types.ToolResultContent:
			if p == nil || !p.ProviderExecuted {
				continue
			}
			filtered = append(filtered, normalizeToolResultContent(*p))
			continue
		case types.ToolErrorContent:
			if !p.ProviderExecuted {
				continue
			}
			filtered = append(filtered, normalizeToolErrorContent(p, true))
			continue
		case *types.ToolErrorContent:
			if p == nil || !p.ProviderExecuted {
				continue
			}
			filtered = append(filtered, normalizeToolErrorContent(*p, true))
			continue
		case types.ToolApprovalResponseContent, *types.ToolApprovalResponseContent:
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered
}

func providerMetadataOptions(metadata json.RawMessage) map[string]interface{} {
	if len(metadata) == 0 || string(metadata) == "null" {
		return nil
	}
	var options map[string]interface{}
	if err := json.Unmarshal(metadata, &options); err != nil || len(options) == 0 {
		return nil
	}
	return options
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

func responseTextContent(part types.TextContent) types.TextContent {
	part.ProviderOptions = providerMetadataOptions(part.ProviderMetadata)
	part.ProviderMetadata = nil
	return part
}

func responseReasoningContent(part types.ReasoningContent) types.ReasoningContent {
	part.ProviderOptions = providerMetadataOptions(part.ProviderMetadata)
	part.ProviderMetadata = nil
	return part
}

func responseFileContent(part types.FileContent) types.FileContent {
	part.ProviderOptions = providerMetadataOptions(part.ProviderMetadata)
	part.ProviderMetadata = nil
	return part
}

func responseGeneratedFileContent(part types.GeneratedFileContent) types.FileContent {
	file := types.FileContent{
		MediaType:        part.MediaType,
		Data:             generatedFileReplayData(part),
		ProviderOptions:  providerMetadataOptions(part.ProviderMetadata),
		ProviderMetadata: nil,
	}
	if len(file.Data) == 0 && part.FileData.Type != "" {
		file.FileData = part.FileData
	}
	return file
}

func responseCustomContent(part types.CustomContent) types.CustomContent {
	part.ProviderOptions = providerMetadataOptions(part.ProviderMetadata)
	part.ProviderMetadata = nil
	return part
}

func responseReasoningFileContent(part types.ReasoningFileContent) types.ReasoningFileContent {
	part.ProviderOptions = providerMetadataOptions(part.ProviderMetadata)
	part.ProviderMetadata = nil
	return part
}

func generatedFileReplayData(part types.GeneratedFileContent) []byte {
	if len(part.Data) > 0 {
		return part.Data
	}
	if len(part.FileData.Data) > 0 {
		return part.FileData.Data
	}
	if part.FileData.DataString != "" {
		if decoded, err := types.DecodeFileDataString(part.FileData.DataString); err == nil {
			return decoded
		}
	}
	return nil
}

func responseToolCallContent(part types.ToolCallContent) types.ToolCallContent {
	part = normalizeResponseToolCallContent(part)
	part.Title = ""
	part.ToolMetadata = nil
	part.Dynamic = false
	part.Invalid = false
	part.Error = nil
	part.ProviderOptions = providerMetadataOptions(part.ProviderMetadata)
	part.ProviderMetadata = nil
	return part
}

func responseToolApprovalRequestContent(part types.ToolApprovalRequestContent) types.ToolApprovalRequestContent {
	toolCallID := part.ToolCallID
	if toolCallID == "" {
		toolCallID = part.ToolCall.ID
	}
	return types.ToolApprovalRequestContent{
		ApprovalID:  part.ApprovalID,
		ToolCallID:  toolCallID,
		Signature:   part.Signature,
		IsAutomatic: part.IsAutomatic,
	}
}

func responseToolApprovalResponseContent(part types.ToolApprovalResponseContent) types.ToolApprovalResponseContent {
	return types.ToolApprovalResponseContent{
		ApprovalID:       part.ApprovalID,
		Approved:         part.Approved,
		Reason:           part.Reason,
		ProviderExecuted: part.ProviderExecuted,
	}
}

func normalizeResponseToolCallContent(part types.ToolCallContent) types.ToolCallContent {
	if part.Input != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(part.Input), &parsed); err != nil {
			part.Arguments = map[string]interface{}{}
			return part
		}
		if parsed == nil {
			parsed = map[string]interface{}{}
		}
		part.Arguments = parsed
	}
	if part.Arguments == nil {
		part.Arguments = map[string]interface{}{}
	}
	return part
}
