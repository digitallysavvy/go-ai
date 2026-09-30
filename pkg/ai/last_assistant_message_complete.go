package ai

// lastStepToolParts returns the tool parts (static or dynamic) that appear
// after the last step-start part of message, in order. If message has no
// step-start part, all tool parts in the message are returned. Shared by
// LastAssistantMessageIsCompleteWithToolCalls and
// LastAssistantMessageIsCompleteWithApprovalResponses, mirroring the
// `lastStepStartIndex`/`slice` logic duplicated in both TS source files.
func lastStepToolParts(message UIMessage) []*ToolUIPart {
	lastStepStartIndex := -1
	for i, part := range message.Parts {
		if partType(part) == "step-start" {
			lastStepStartIndex = i
		}
	}

	var tools []*ToolUIPart
	for _, part := range message.Parts[lastStepStartIndex+1:] {
		if tool, ok := AsToolUIPart(part); ok {
			tools = append(tools, tool)
		}
	}
	return tools
}

// toolOutputIsTerminalAndNotPreliminary reports whether a tool part in the
// output-available state represents a final (non-preliminary) result.
func toolOutputAvailableAndFinal(tool *ToolUIPart) bool {
	return tool.State == ToolStateOutputAvailable && (tool.Preliminary == nil || !*tool.Preliminary)
}

// LastAssistantMessageIsCompleteWithToolCalls reports whether the last
// message in messages is an assistant message whose last step has at least
// one non-provider-executed tool invocation, and every such tool invocation
// has reached a terminal state (a final, non-preliminary output-available
// result, or output-error).
//
// This is a pure, framework-agnostic helper used to decide whether to
// auto-continue/re-submit a conversation after tool calls complete. Mirrors
// TS lastAssistantMessageIsCompleteWithToolCalls
// (ui/last-assistant-message-is-complete-with-tool-calls.ts).
func LastAssistantMessageIsCompleteWithToolCalls(messages []UIMessage) bool {
	if len(messages) == 0 {
		return false
	}
	message := messages[len(messages)-1]
	if message.Role != UIMessageRoleAssistant {
		return false
	}

	found := false
	for _, tool := range lastStepToolParts(message) {
		if tool.ProviderExecuted != nil && *tool.ProviderExecuted {
			continue
		}
		found = true
		if tool.State != ToolStateOutputError && !toolOutputAvailableAndFinal(tool) {
			return false
		}
	}
	return found
}

// LastAssistantMessageIsCompleteWithApprovalResponses reports whether the
// last message in messages is an assistant message whose last step has at
// least one tool invocation in the approval-responded state, and every tool
// invocation in that step has reached a terminal state (a final,
// non-preliminary output-available result, output-error, output-denied, or
// approval-responded). Unlike LastAssistantMessageIsCompleteWithToolCalls,
// provider-executed tool invocations are not excluded.
//
// This is a pure, framework-agnostic helper used to decide whether to
// auto-continue/re-submit a conversation after tool call approvals are
// resolved. Mirrors TS lastAssistantMessageIsCompleteWithApprovalResponses
// (ui/last-assistant-message-is-complete-with-approval-responses.ts).
func LastAssistantMessageIsCompleteWithApprovalResponses(messages []UIMessage) bool {
	if len(messages) == 0 {
		return false
	}
	message := messages[len(messages)-1]
	if message.Role != UIMessageRoleAssistant {
		return false
	}

	toolParts := lastStepToolParts(message)

	hasApprovalResponse := false
	for _, tool := range toolParts {
		if tool.State == ToolStateApprovalResponded {
			hasApprovalResponse = true
			break
		}
	}
	if !hasApprovalResponse {
		return false
	}

	for _, tool := range toolParts {
		switch tool.State {
		case ToolStateOutputAvailable:
			if !toolOutputAvailableAndFinal(tool) {
				return false
			}
		case ToolStateOutputError, ToolStateOutputDenied, ToolStateApprovalResponded:
			// terminal; keep checking the rest
		default:
			return false
		}
	}
	return true
}
