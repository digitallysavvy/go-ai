package ai

// lastStepParts returns the parts of message that appear after its last
// step-start part, in order. If message has no step-start part, all parts
// in the message are returned. Mirrors the `lastStepStartIndex`/`slice`
// logic shared by lastAssistantMessageIsCompleteWithToolCalls and
// lastAssistantMessageIsCompleteWithApprovalResponses in TS.
func lastStepParts(message UIMessage) []UIMessagePart {
	lastStepStartIndex := -1
	for i, part := range message.Parts {
		if partType(part) == "step-start" {
			lastStepStartIndex = i
		}
	}
	return message.Parts[lastStepStartIndex+1:]
}

// lastStepToolParts returns the tool parts (static or dynamic) that appear
// after the last step-start part of message, in order. If message has no
// step-start part, all tool parts in the message are returned. Shared by
// LastAssistantMessageIsCompleteWithToolCalls and
// LastAssistantMessageIsCompleteWithApprovalResponses.
func lastStepToolParts(message UIMessage) []*ToolUIPart {
	var tools []*ToolUIPart
	for _, part := range lastStepParts(message) {
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
// one non-provider-executed tool invocation, every such tool invocation has
// reached a terminal state (a final, non-preliminary output-available
// result, or output-error), and no text part after the last such tool
// invocation is still streaming (state other than "done"). Completed model
// text may follow the tool invocation, since client-side tool results
// update parts in place, but trailing text without a completed stream state
// (for example terminal error text appended after the tool call) must
// prevent automatic resubmission.
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

	parts := lastStepParts(message)

	found := false
	lastToolInvocationIndex := -1
	for i, part := range parts {
		tool, ok := AsToolUIPart(part)
		if !ok {
			continue
		}
		if tool.ProviderExecuted != nil && *tool.ProviderExecuted {
			continue
		}
		found = true
		lastToolInvocationIndex = i
		if tool.State != ToolStateOutputError && !toolOutputAvailableAndFinal(tool) {
			return false
		}
	}
	if !found {
		return false
	}

	for _, part := range parts[lastToolInvocationIndex+1:] {
		text, ok := asTextUIPart(part)
		if ok && text.State != UIPartStateDone {
			return false
		}
	}
	return true
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
