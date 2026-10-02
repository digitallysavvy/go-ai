package ai

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// PruneToolCallsRule describes one tool-call pruning pass.
//
// Type is one of:
//   - "all": prune across the entire message list (no protected window).
//   - "before-last-message": protect only the very last message.
//   - "before-last-<N>-messages": protect the last N messages (e.g.
//     "before-last-3-messages"). N == 0 reproduces a TypeScript quirk: see
//     the PruneModelMessages doc comment.
//
// Tools, when non-nil, is a denylist: only parts whose tool name appears in
// Tools are pruned; parts for other tools are kept. When Tools is nil, every
// tool part outside the protected window is pruned unconditionally.
//
// Mirrors the TS pruneMessages toolCalls rule object
// (generate-text/prune-messages.ts). "before-last-0-messages" behaves
// exactly like "all" (TS #21732): it protects no trailing messages and
// contributes no IDs to the kept-ID scan.
type PruneToolCallsRule struct {
	Type  string
	Tools []string
}

// PruneModelMessagesOptions configures PruneModelMessages.
type PruneModelMessagesOptions struct {
	// Reasoning controls removal of reasoning content from assistant
	// messages: "none" (default), "all", or "before-last-message" (removes
	// reasoning from every assistant message except the last one).
	Reasoning string

	// ToolCalls is an ordered list of pruning rules applied in sequence.
	// The default (nil/empty) prunes no tool calls, matching TS's default
	// `toolCalls: []`.
	ToolCalls []PruneToolCallsRule

	// EmptyMessages is "remove" (default) or "keep": whether messages left
	// with no content and no top-level tool calls after pruning are dropped.
	EmptyMessages string
}

// PruneModelMessages prunes reasoning and/or tool-call content from a list
// of model messages, mirroring the TypeScript SDK's `pruneMessages`
// (generate-text/prune-messages.ts). It never mutates the input messages or
// their content slices.
//
// This is distinct from PruneMessages (pruning.go), which trims history to
// fit a token budget. PruneModelMessages instead removes specific *kinds* of
// content (reasoning, tool calls/results/approvals) regardless of size, to
// keep replayed history small or avoid re-sending stale tool context.
//
// # "before-last-0-messages" equals "all" (TS #21732)
//
// TS's pruneMessages used to compute the protected window as
// `messages.slice(-keepLastMessagesCount)`. For N == 0, JavaScript's
// `slice(-0)` is `slice(0)` (negative zero equals positive zero), which
// returned the *entire* array — not an empty one — so the kept-ID scan
// collected every tool call/approval ID, as if nothing were pruned, even
// though the later "is this message in the protected window" check already
// treated N == 0 as falsy (protecting no message from filtering). The net
// effect was that "before-last-0-messages" kept virtually every tool call
// anyway, making the rule close to a no-op. TS #21732 fixed this by also
// skipping the kept-ID scan when the parsed count is zero, so
// "before-last-0-messages" now behaves exactly like "all". Go mirrors the
// fixed behavior.
func PruneModelMessages(messages []types.Message, opts PruneModelMessagesOptions) ([]types.Message, error) {
	reasoning := opts.Reasoning
	if reasoning == "" {
		reasoning = "none"
	}
	switch reasoning {
	case "none", "all", "before-last-message":
	default:
		return nil, fmt.Errorf("pruneMessages: invalid reasoning value %q", opts.Reasoning)
	}

	emptyMessages := opts.EmptyMessages
	if emptyMessages == "" {
		emptyMessages = "remove"
	}
	if emptyMessages != "remove" && emptyMessages != "keep" {
		return nil, fmt.Errorf("pruneMessages: invalid emptyMessages value %q", opts.EmptyMessages)
	}

	// Copy the message slice (and copy Content/ToolCalls before mutating them)
	// so the input is never modified.
	pruned := make([]types.Message, len(messages))
	copy(pruned, messages)

	if reasoning == "all" || reasoning == "before-last-message" {
		for i, msg := range pruned {
			if msg.Role != types.RoleAssistant || len(msg.Content) == 0 {
				continue
			}
			if reasoning == "before-last-message" && i == len(pruned)-1 {
				continue
			}
			newContent := make([]types.ContentPart, 0, len(msg.Content))
			for _, part := range msg.Content {
				if part.ContentType() == "reasoning" || part.ContentType() == "reasoning-file" {
					continue
				}
				newContent = append(newContent, part)
			}
			msg.Content = newContent
			pruned[i] = msg
		}
	}

	for _, rule := range opts.ToolCalls {
		var err error
		pruned, err = applyPruneToolCallsRule(pruned, rule)
		if err != nil {
			return nil, err
		}
	}

	if emptyMessages == "remove" {
		filtered := make([]types.Message, 0, len(pruned))
		for _, msg := range pruned {
			if len(msg.Content) == 0 && len(msg.ToolCalls) == 0 {
				continue
			}
			filtered = append(filtered, msg)
		}
		pruned = filtered
	}

	return pruned, nil
}

// isToolCallPart reports whether part is a tool-call/tool-result/tool-error
// part and, if so, returns its tool call ID and tool name. ToolErrorContent
// is a Go-only addition alongside the TS tool-call/tool-result pair.
func isToolCallPart(part types.ContentPart) (toolCallID, toolName string, ok bool) {
	switch p := part.(type) {
	case types.ToolCallContent:
		return p.ToolCallID, p.ToolName, true
	case types.ToolResultContent:
		return p.ToolCallID, p.ToolName, true
	case types.ToolErrorContent:
		return p.ToolCallID, p.ToolName, true
	default:
		return "", "", false
	}
}

// approvalRequestIDs returns the approval ID and originating tool call ID
// for a tool-approval-request part. The tool call ID falls back to
// ToolCall.ID when ToolCallID is empty (some code paths normalize the call
// only onto the nested ToolCall).
func approvalRequestIDs(p types.ToolApprovalRequestContent) (approvalID, toolCallID string) {
	toolCallID = p.ToolCallID
	if toolCallID == "" {
		toolCallID = p.ToolCall.ID
	}
	return p.ApprovalID, toolCallID
}

func applyPruneToolCallsRule(messages []types.Message, rule PruneToolCallsRule) ([]types.Message, error) {
	keepLast, err := resolveKeepLastMessagesCount(rule.Type)
	if err != nil {
		return nil, err
	}

	keptToolCallIDs := map[string]bool{}
	keptApprovalIDs := map[string]bool{}

	if keepLast != nil && *keepLast != 0 {
		// *keepLast > 0 here (resolveKeepLastMessagesCount never returns a
		// negative value, and the zero case is excluded above per TS #21732:
		// see the doc comment on PruneModelMessages).
		scanFrom := len(messages) - *keepLast
		if scanFrom < 0 {
			scanFrom = 0
		}
		for _, msg := range messages[scanFrom:] {
			if msg.Role != types.RoleAssistant && msg.Role != types.RoleTool {
				continue
			}
			for _, part := range msg.Content {
				if toolCallID, _, ok := isToolCallPart(part); ok {
					keptToolCallIDs[toolCallID] = true
					continue
				}
				switch p := part.(type) {
				case types.ToolApprovalRequestContent:
					approvalID, _ := approvalRequestIDs(p)
					keptApprovalIDs[approvalID] = true
				case types.ToolApprovalResponseContent:
					keptApprovalIDs[p.ApprovalID] = true
				}
			}
		}
	}

	// Build global maps (across the whole message list, regardless of the
	// protected window) from tool call ID / approval ID to tool name. These
	// must be global because a tool-approval-response can live in a
	// different message than its tool-approval-request.
	toolCallIDToName := map[string]string{}
	for _, msg := range messages {
		if msg.Role != types.RoleAssistant && msg.Role != types.RoleTool {
			continue
		}
		for _, part := range msg.Content {
			if toolCallID, toolName, ok := isToolCallPart(part); ok {
				toolCallIDToName[toolCallID] = toolName
			}
		}
	}

	approvalIDToToolCallID := map[string]string{}
	approvalIDToToolName := map[string]string{}
	for _, msg := range messages {
		if msg.Role != types.RoleAssistant && msg.Role != types.RoleTool {
			continue
		}
		for _, part := range msg.Content {
			req, ok := part.(types.ToolApprovalRequestContent)
			if !ok {
				continue
			}
			approvalID, toolCallID := approvalRequestIDs(req)
			approvalIDToToolCallID[approvalID] = toolCallID
			if name, ok := toolCallIDToName[toolCallID]; ok {
				approvalIDToToolName[approvalID] = name
			}
		}
	}

	// A kept approval response depends on its originating tool call to
	// resolve the pending approval; keep that call too.
	for approvalID := range keptApprovalIDs {
		if toolCallID, ok := approvalIDToToolCallID[approvalID]; ok && toolCallID != "" {
			keptToolCallIDs[toolCallID] = true
		}
	}

	shouldKeepPart := func(part types.ContentPart) bool {
		if toolCallID, toolName, ok := isToolCallPart(part); ok {
			if keptToolCallIDs[toolCallID] {
				return true
			}
			return len(rule.Tools) > 0 && toolName != "" && !containsString(rule.Tools, toolName)
		}
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			approvalID, _ := approvalRequestIDs(p)
			if keptApprovalIDs[approvalID] {
				return true
			}
			toolName := approvalIDToToolName[approvalID]
			return len(rule.Tools) > 0 && toolName != "" && !containsString(rule.Tools, toolName)
		case types.ToolApprovalResponseContent:
			if keptApprovalIDs[p.ApprovalID] {
				return true
			}
			toolName := approvalIDToToolName[p.ApprovalID]
			return len(rule.Tools) > 0 && toolName != "" && !containsString(rule.Tools, toolName)
		default:
			// Not a tool-related part: always kept.
			return true
		}
	}

	out := make([]types.Message, len(messages))
	copy(out, messages)

	for i, msg := range out {
		if msg.Role != types.RoleAssistant && msg.Role != types.RoleTool {
			continue
		}
		// TS: `keepLastMessagesCount && messageIndex >= ...` — 0 (and nil,
		// the "all" case) are falsy, so no message is protected from
		// filtering in either case.
		if keepLast != nil && *keepLast != 0 && i >= len(out)-*keepLast {
			continue
		}

		if len(msg.Content) > 0 {
			newContent := make([]types.ContentPart, 0, len(msg.Content))
			for _, part := range msg.Content {
				if shouldKeepPart(part) {
					newContent = append(newContent, part)
				}
			}
			msg.Content = newContent
		}

		// Go-only extension: apply the same rule to the legacy top-level
		// ToolCalls field.
		if len(msg.ToolCalls) > 0 {
			newCalls := make([]types.ToolCall, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				if keptToolCallIDs[call.ID] {
					newCalls = append(newCalls, call)
					continue
				}
				if len(rule.Tools) > 0 && call.ToolName != "" && !containsString(rule.Tools, call.ToolName) {
					newCalls = append(newCalls, call)
				}
			}
			msg.ToolCalls = newCalls
		}

		out[i] = msg
	}

	return out, nil
}

// resolveKeepLastMessagesCount parses a PruneToolCallsRule.Type into the
// number of trailing messages to protect. nil means "all" (no protected
// window, and the kept-ID scan below covers zero messages rather than the
// whole list).
func resolveKeepLastMessagesCount(ruleType string) (*int, error) {
	switch ruleType {
	case "all":
		return nil, nil
	case "before-last-message":
		one := 1
		return &one, nil
	}
	const prefix, suffix = "before-last-", "-messages"
	if strings.HasPrefix(ruleType, prefix) && strings.HasSuffix(ruleType, suffix) {
		numStr := strings.TrimSuffix(strings.TrimPrefix(ruleType, prefix), suffix)
		if n, err := strconv.Atoi(numStr); err == nil && n >= 0 {
			return &n, nil
		}
	}
	return nil, fmt.Errorf("pruneMessages: invalid toolCalls rule type %q", ruleType)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
