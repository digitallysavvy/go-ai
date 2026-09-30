package harness

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// CollectToolResultContinuations extracts client-provided tool results from
// the trailing tool message. Mirrors TS
// `collectHarnessAgentToolResultContinuations`.
func CollectToolResultContinuations(messages []types.Message) []types.ToolResultContent {
	if len(messages) == 0 {
		return nil
	}
	last := messages[len(messages)-1]
	if last.Role != types.RoleTool {
		return nil
	}
	var out []types.ToolResultContent
	for _, part := range last.Content {
		if tr, ok := part.(types.ToolResultContent); ok {
			out = append(out, tr)
		}
	}
	return out
}

// CollectToolApprovalContinuations extracts approval decisions that should
// continue a suspended harness turn.
//
// AI SDK clients send approval decisions as a trailing role:"tool" message
// containing tool-approval-response parts. The response only carries the
// approval id, so the harness has to recover the matching approval request
// locally to find the original tool call before it can resume the paused
// turn. Responses that already have a tool result are ignored, because those
// approvals were already consumed by a prior continuation. Mirrors TS
// `collectHarnessAgentToolApprovalContinuations`.
func CollectToolApprovalContinuations(messages []types.Message) ([]types.ToolApprovalResponseContent, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	last := messages[len(messages)-1]
	if last.Role != types.RoleTool {
		return nil, nil
	}

	toolCallIDs := map[string]struct{}{}
	approvalRequestsByID := map[string]types.ToolApprovalRequestContent{}
	for _, message := range messages {
		if message.Role != types.RoleAssistant {
			continue
		}
		for _, part := range message.Content {
			switch p := part.(type) {
			case types.ToolCallContent:
				toolCallIDs[p.ToolCallID] = struct{}{}
			case types.ToolApprovalRequestContent:
				approvalRequestsByID[p.ApprovalID] = p
			}
		}
	}

	toolResultIDs := map[string]struct{}{}
	for _, part := range last.Content {
		if tr, ok := part.(types.ToolResultContent); ok {
			toolResultIDs[tr.ToolCallID] = struct{}{}
		}
	}

	var continuations []types.ToolApprovalResponseContent
	for _, part := range last.Content {
		response, ok := part.(types.ToolApprovalResponseContent)
		if !ok {
			continue
		}

		approvalRequest, ok := approvalRequestsByID[response.ApprovalID]
		if !ok {
			return nil, NewHarnessError(
				fmt.Sprintf("Tool approval response '%s' does not match a prior tool approval request.", response.ApprovalID),
				nil,
			)
		}
		if _, done := toolResultIDs[approvalRequest.ToolCallID]; done {
			continue
		}
		if _, ok := toolCallIDs[approvalRequest.ToolCallID]; !ok {
			return nil, NewHarnessError(
				fmt.Sprintf("Tool approval request '%s' references unknown tool call '%s'.", approvalRequest.ApprovalID, approvalRequest.ToolCallID),
				nil,
			)
		}

		continuations = append(continuations, response)
	}

	return continuations, nil
}
