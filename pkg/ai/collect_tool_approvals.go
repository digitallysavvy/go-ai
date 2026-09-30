package ai

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// InvalidToolApprovalError is returned when a tool-approval-response in the
// input messages references an approval ID with no matching
// tool-approval-request. Mirrors TS InvalidToolApprovalError.
type InvalidToolApprovalError struct {
	ApprovalID string
}

func (e *InvalidToolApprovalError) Error() string {
	return fmt.Sprintf(`Tool approval response references unknown approvalId: "%s". No matching tool-approval-request found in message history.`, e.ApprovalID)
}

// IsInvalidToolApprovalError reports whether err is an InvalidToolApprovalError.
func IsInvalidToolApprovalError(err error) bool {
	var target *InvalidToolApprovalError
	return errors.As(err, &target)
}

// ToolCallNotFoundForApprovalError is returned when an approval request
// references a tool call that does not exist in the message history. Mirrors
// TS ToolCallNotFoundForApprovalError.
type ToolCallNotFoundForApprovalError struct {
	ToolCallID string
	ApprovalID string
}

func (e *ToolCallNotFoundForApprovalError) Error() string {
	return fmt.Sprintf(`Tool call "%s" not found for approval request "%s".`, e.ToolCallID, e.ApprovalID)
}

// IsToolCallNotFoundForApprovalError reports whether err is a
// ToolCallNotFoundForApprovalError.
func IsToolCallNotFoundForApprovalError(err error) bool {
	var target *ToolCallNotFoundForApprovalError
	return errors.As(err, &target)
}

// CollectedToolApproval pairs an approval request/response from the message
// history with the tool call it refers to. Mirrors TS CollectedToolApprovals.
type CollectedToolApproval struct {
	ApprovalRequest  types.ToolApprovalRequestContent
	ApprovalResponse types.ToolApprovalResponseContent
	ToolCall         types.ToolCall
	// ExistingToolResult is the tool result for the call that is already
	// present in the last tool message, if any.
	ExistingToolResult *types.ToolResultContent
}

// CollectToolApprovalsResult is the result of CollectToolApprovals.
type CollectToolApprovalsResult struct {
	ApprovedToolApprovals []CollectedToolApproval
	DeniedToolApprovals   []CollectedToolApproval
}

// CollectToolApprovals collects the tool approval responses from the last
// message when it is a tool message. Approved responses whose tool call
// already has a result are skipped; denied responses are skipped only when an
// existing result is not an execution-denied output (so a client denial can
// override an earlier synthetic denial). Mirrors TS collectToolApprovals.
func CollectToolApprovals(messages []types.Message) (CollectToolApprovalsResult, error) {
	var result CollectToolApprovalsResult
	if len(messages) == 0 || messages[len(messages)-1].Role != types.RoleTool {
		return result, nil
	}
	lastMessage := messages[len(messages)-1]

	// Tool calls and approval requests from assistant messages. Go maps are
	// exact-key, so client-supplied IDs cannot resolve inherited properties
	// (the TS Object.create(null) concern does not apply).
	toolCallsByID := map[string]types.ToolCall{}
	requestsByApprovalID := map[string]types.ToolApprovalRequestContent{}
	for _, message := range messages {
		if message.Role != types.RoleAssistant {
			continue
		}
		for _, call := range message.ToolCalls {
			toolCallsByID[call.ID] = call
		}
		for _, part := range message.Content {
			switch p := part.(type) {
			case types.ToolCallContent:
				call := toolCallFromContentPart(p)
				toolCallsByID[call.ID] = call
			case *types.ToolCallContent:
				if p != nil {
					call := toolCallFromContentPart(*p)
					toolCallsByID[call.ID] = call
				}
			}
		}
		for _, part := range message.Content {
			switch p := part.(type) {
			case types.ToolApprovalRequestContent:
				requestsByApprovalID[p.ApprovalID] = normalizeApprovalRequest(p)
			case *types.ToolApprovalRequestContent:
				if p != nil {
					requestsByApprovalID[p.ApprovalID] = normalizeApprovalRequest(*p)
				}
			}
		}
	}

	// Tool results from the last tool message.
	toolResults := map[string]types.ToolResultContent{}
	for _, part := range lastMessage.Content {
		switch p := part.(type) {
		case types.ToolResultContent:
			toolResults[p.ToolCallID] = p
		case *types.ToolResultContent:
			if p != nil {
				toolResults[p.ToolCallID] = *p
			}
		case types.ToolErrorContent:
			toolResults[p.ToolCallID] = toolResultFromErrorContent(p)
		case *types.ToolErrorContent:
			if p != nil {
				toolResults[p.ToolCallID] = toolResultFromErrorContent(*p)
			}
		}
	}

	for _, part := range lastMessage.Content {
		var response types.ToolApprovalResponseContent
		switch p := part.(type) {
		case types.ToolApprovalResponseContent:
			response = p
		case *types.ToolApprovalResponseContent:
			if p == nil {
				continue
			}
			response = *p
		default:
			continue
		}

		request, ok := requestsByApprovalID[response.ApprovalID]
		if !ok {
			return CollectToolApprovalsResult{}, &InvalidToolApprovalError{ApprovalID: response.ApprovalID}
		}

		var existing *types.ToolResultContent
		if tr, ok := toolResults[request.ToolCallID]; ok {
			trCopy := tr
			existing = &trCopy
			if response.Approved || !isExecutionDeniedToolResult(tr) {
				continue
			}
		}

		toolCall, ok := toolCallsByID[request.ToolCallID]
		if !ok {
			return CollectToolApprovalsResult{}, &ToolCallNotFoundForApprovalError{
				ToolCallID: request.ToolCallID,
				ApprovalID: request.ApprovalID,
			}
		}

		approval := CollectedToolApproval{
			ApprovalRequest:    request,
			ApprovalResponse:   response,
			ToolCall:           toolCall,
			ExistingToolResult: existing,
		}
		if response.Approved {
			result.ApprovedToolApprovals = append(result.ApprovedToolApprovals, approval)
		} else {
			result.DeniedToolApprovals = append(result.DeniedToolApprovals, approval)
		}
	}
	return result, nil
}

func isExecutionDeniedToolResult(tr types.ToolResultContent) bool {
	if tr.Output != nil {
		return tr.Output.Type == types.ToolResultOutputExecutionDenied
	}
	switch v := tr.Result.(type) {
	case types.ToolResultOutput:
		return v.Type == types.ToolResultOutputExecutionDenied
	case *types.ToolResultOutput:
		return v != nil && v.Type == types.ToolResultOutputExecutionDenied
	}
	return false
}

func toolResultFromErrorContent(p types.ToolErrorContent) types.ToolResultContent {
	return types.ToolResultContent{
		ToolCallID: p.ToolCallID,
		ToolName:   p.ToolName,
		Output:     &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: fmt.Sprint(p.Error)},
	}
}

// normalizeApprovalRequest fills ToolCallID from the embedded Go ToolCall
// when only the latter is present.
func normalizeApprovalRequest(p types.ToolApprovalRequestContent) types.ToolApprovalRequestContent {
	if p.ToolCallID == "" {
		p.ToolCallID = p.ToolCall.ID
	}
	return p
}

// toolCallFromContentPart converts a model-message tool-call part into a
// ToolCall. The decoded Arguments win; otherwise the raw JSON Input is parsed.
func toolCallFromContentPart(part types.ToolCallContent) types.ToolCall {
	args := part.Arguments
	if args == nil && part.Input != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(part.Input), &parsed); err == nil {
			args = parsed
		}
	}
	var providerMetadata map[string]interface{}
	if len(part.ProviderMetadata) > 0 {
		_ = json.Unmarshal(part.ProviderMetadata, &providerMetadata)
	}
	return types.ToolCall{
		ID:               part.ToolCallID,
		ToolName:         part.ToolName,
		Title:            part.Title,
		Arguments:        args,
		RawArguments:     part.Input,
		ProviderExecuted: part.ProviderExecuted,
		ProviderMetadata: providerMetadata,
		ToolMetadata:     part.ToolMetadata,
		ThoughtSignature: part.ThoughtSignature,
		Dynamic:          part.Dynamic,
		Invalid:          part.Invalid,
	}
}
