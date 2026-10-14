package prompt

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// NewMissingToolResultsError builds a MissingToolResultsError whose message
// matches the TypeScript SDK's AI_MissingToolResultsError text.
func NewMissingToolResultsError(toolCallIDs []string) *types.MissingToolResultsError {
	plural := len(toolCallIDs) > 1
	verb, calls := " is", ""
	if plural {
		verb, calls = "s are", "s"
	}
	return &types.MissingToolResultsError{
		ToolCallIDs: toolCallIDs,
		Message:     fmt.Sprintf("Tool result%s missing for tool call%s %s.", verb, calls, strings.Join(toolCallIDs, ", ")),
	}
}

// ValidateToolResults returns a *types.MissingToolResultsError when an
// assistant tool call that the SDK does not execute at the provider has no
// tool result before the next user or system message, or before the end of the
// conversation. Mirrors the validation in TS convertToLanguageModelPrompt. A
// tool approval response does not count as a result (TS #22449): an approval
// that is not resolved by a tool result before later context is rejected.
// Provider-executed tool calls are exempt, as are calls to a tool in
// providerExecutedTools (tool names whose definition is provider-executed;
// Go providers do not always flag every tool call part).
func ValidateToolResults(messages []types.Message, providerExecutedTools ...string) error {
	exempt := map[string]bool{}
	exemptTool := map[string]bool{}
	for _, name := range providerExecutedTools {
		exemptTool[name] = true
	}
	for _, message := range messages {
		for _, part := range message.Content {
			switch p := part.(type) {
			case types.ToolApprovalResponseContent:
				if p.ProviderExecuted {
					exempt[p.ToolCallID] = true
				}
			case *types.ToolApprovalResponseContent:
				if p != nil && p.ProviderExecuted {
					exempt[p.ToolCallID] = true
				}
			}
		}
	}

	var pending []string
	pendingSet := map[string]bool{}
	add := func(id string) {
		if id == "" || exempt[id] || pendingSet[id] {
			return
		}
		pendingSet[id] = true
		pending = append(pending, id)
	}
	remove := func(id string) {
		if !pendingSet[id] {
			return
		}
		delete(pendingSet, id)
		for i, p := range pending {
			if p == id {
				pending = append(pending[:i], pending[i+1:]...)
				break
			}
		}
	}

	for _, message := range messages {
		switch message.Role {
		case types.RoleAssistant:
			for _, call := range message.ToolCalls {
				if !call.ProviderExecuted && !exemptTool[call.ToolName] {
					add(call.ID)
				}
			}
			for _, part := range message.Content {
				switch p := part.(type) {
				case types.ToolCallContent:
					if !p.ProviderExecuted && !exemptTool[p.ToolName] {
						add(p.ToolCallID)
					}
				case *types.ToolCallContent:
					if p != nil && !p.ProviderExecuted && !exemptTool[p.ToolName] {
						add(p.ToolCallID)
					}
				}
			}
		case types.RoleTool:
			for _, part := range message.Content {
				switch p := part.(type) {
				case types.ToolResultContent:
					remove(p.ToolCallID)
				case *types.ToolResultContent:
					if p != nil {
						remove(p.ToolCallID)
					}
				case types.ToolErrorContent:
					remove(p.ToolCallID)
				case *types.ToolErrorContent:
					if p != nil {
						remove(p.ToolCallID)
					}
				}
			}
		case types.RoleUser, types.RoleSystem:
			if len(pending) > 0 {
				return NewMissingToolResultsError(append([]string(nil), pending...))
			}
		}
	}
	if len(pending) > 0 {
		return NewMissingToolResultsError(pending)
	}
	return nil
}
