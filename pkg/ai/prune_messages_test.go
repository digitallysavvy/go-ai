package ai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func userMsg(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}
}

func assistantMsg(parts ...types.ContentPart) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: parts}
}

func toolMsg(parts ...types.ContentPart) types.Message {
	return types.Message{Role: types.RoleTool, Content: parts}
}

// TS prune-messages.test.ts "reasoning > should prune all reasoning parts"
func TestPruneModelMessages_ReasoningAll(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		userMsg("Weather?"),
		assistantMsg(
			types.ReasoningContent{Text: "thinking..."},
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "get-weather"},
		),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{Reasoning: "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out[1].Content) != 1 {
		t.Fatalf("expected reasoning to be pruned, got %+v", out[1].Content)
	}
	if _, ok := out[1].Content[0].(types.ToolCallContent); !ok {
		t.Errorf("expected the remaining part to be the tool call, got %T", out[1].Content[0])
	}
	// Input must not be mutated.
	if len(messages[1].Content) != 2 {
		t.Errorf("input messages were mutated: %+v", messages[1].Content)
	}
}

// TS prune-messages.test.ts "reasoning > should prune reasoning files while
// preserving regular files and text"
func TestPruneModelMessages_ReasoningAll_PreservesFilesAndText(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		assistantMsg(
			types.TextContent{Text: "here you go"},
			types.ReasoningFileContent{MediaType: "image/png", Data: []byte("x")},
			types.FileContent{MediaType: "image/png", Data: []byte("y")},
		),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{Reasoning: "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out[0].Content) != 2 {
		t.Fatalf("expected reasoning-file to be pruned, kept: %+v", out[0].Content)
	}
	for _, part := range out[0].Content {
		if part.ContentType() == "reasoning-file" {
			t.Errorf("reasoning-file part survived pruning: %+v", part)
		}
	}
}

// TS prune-messages.test.ts "reasoning > should remove messages containing
// only reasoning text and files" (combined with default emptyMessages=remove)
func TestPruneModelMessages_ReasoningAll_RemovesNowEmptyMessages(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		userMsg("hi"),
		assistantMsg(types.ReasoningContent{Text: "thinking"}),
		assistantMsg(types.TextContent{Text: "answer"}),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{Reasoning: "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected the reasoning-only message to be dropped, got %d messages", len(out))
	}
}

// TS prune-messages.test.ts "reasoning > should prune the trailing message"
// (before-last-message keeps reasoning in the last assistant message).
func TestPruneModelMessages_ReasoningBeforeLastMessage(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		assistantMsg(types.ReasoningContent{Text: "first"}, types.TextContent{Text: "a"}),
		assistantMsg(types.ReasoningContent{Text: "last"}, types.TextContent{Text: "b"}),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{Reasoning: "before-last-message"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out[0].Content) != 1 {
		t.Fatalf("expected reasoning removed from the first message, got %+v", out[0].Content)
	}
	if len(out[1].Content) != 2 {
		t.Fatalf("expected reasoning kept in the last message, got %+v", out[1].Content)
	}
}

// TS prune-messages.test.ts "toolCalls > all > should prune all tool calls,
// results, errors, and approvals"
func TestPruneModelMessages_ToolCallsAll_NoToolsFilter(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		userMsg("Weather in Tokyo and Busan?"),
		assistantMsg(
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather-1"},
			types.ToolCallContent{ToolCallID: "call-2", ToolName: "weather-2"},
			types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-2"},
		),
		toolMsg(
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
			types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather-1"},
			types.ToolResultContent{ToolCallID: "call-2", ToolName: "weather-2"},
		),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{
		ToolCalls: []PruneToolCallsRule{{Type: "all"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both tool-bearing messages become empty and are dropped (default
	// emptyMessages=remove); only the user message remains.
	if len(out) != 1 {
		t.Fatalf("expected only the user message to remain, got %d: %+v", len(out), out)
	}
}

// TS prune-messages.test.ts "toolCalls > selective tool pruning with
// approvals" — tools denylist prunes only the named tool, keeping its
// sibling and the approval response tied to the surviving tool call.
func TestPruneModelMessages_ToolCallsAll_ToolsDenylist(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		userMsg("Weather in Tokyo and Busan?"),
		assistantMsg(
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather-1"},
			types.ToolCallContent{ToolCallID: "call-2", ToolName: "weather-2"},
			types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-2"},
		),
		toolMsg(
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
			types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather-1"},
			types.ToolResultContent{ToolCallID: "call-2", ToolName: "weather-2"},
		),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{
		ToolCalls: []PruneToolCallsRule{{Type: "all", Tools: []string{"weather-2"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected all 3 messages to survive, got %d", len(out))
	}
	// weather-1's call/result must remain; weather-2's call, its approval
	// request, response, and result must all be gone (no orphaned parts).
	assistantParts := out[1].Content
	if len(assistantParts) != 1 {
		t.Fatalf("expected only the weather-1 call to remain, got %+v", assistantParts)
	}
	if c, ok := assistantParts[0].(types.ToolCallContent); !ok || c.ToolCallID != "call-1" {
		t.Errorf("expected call-1 to survive, got %+v", assistantParts[0])
	}
	toolParts := out[2].Content
	if len(toolParts) != 1 {
		t.Fatalf("expected only the weather-1 result to remain, got %+v", toolParts)
	}
	if r, ok := toolParts[0].(types.ToolResultContent); !ok || r.ToolCallID != "call-1" {
		t.Errorf("expected call-1's result to survive, got %+v", toolParts[0])
	}
}

// TS prune-messages.test.ts "toolCalls > should drop unresolved approval
// responses during selective pruning".
func TestPruneModelMessages_DropsOrphanedApprovalResponse(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		userMsg("Weather?"),
		assistantMsg(types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather-1"}),
		toolMsg(
			types.ToolApprovalResponseContent{ApprovalID: "unknown-approval", Approved: true},
			types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather-1"},
		),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{
		ToolCalls: []PruneToolCallsRule{{Type: "all", Tools: []string{"weather-2"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	toolParts := out[2].Content
	for _, p := range toolParts {
		if p.ContentType() == "tool-approval-response" {
			t.Errorf("unresolved approval response must be dropped, found %+v", p)
		}
	}
}

// TS prune-messages.test.ts "toolCalls > before-last-message > should retain
// the originating tool call for a pending approval response".
func TestPruneModelMessages_RetainsOriginatingCallForKeptApproval(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		userMsg("Weather?"),
		assistantMsg(
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "echo"},
			types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1"},
		),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{
		ToolCalls: []PruneToolCallsRule{{Type: "before-last-message"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(out, messages) {
		t.Fatalf("expected the pending approval and its call to be untouched: %+v", out)
	}
}

// TS prune-messages.test.ts "toolCalls > before-last-2-messages": N > 0
// behaves like a normal protected window. N == 0 behaves exactly like "all"
// (TS #21732): see the PruneModelMessages doc comment for the slice(-0)
// quirk this fixed.
func TestPruneModelMessages_BeforeLastNMessages(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		assistantMsg(types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather"}),
		toolMsg(types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather"}),
		assistantMsg(types.ToolCallContent{ToolCallID: "call-2", ToolName: "weather"}),
		toolMsg(types.ToolResultContent{ToolCallID: "call-2", ToolName: "weather"}),
	}

	t.Run("N=1 protects only the last message", func(t *testing.T) {
		out, err := PruneModelMessages(messages, PruneModelMessagesOptions{
			ToolCalls: []PruneToolCallsRule{{Type: "before-last-1-messages"}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The window scan (the last message only) finds call-2's id, which
		// keeps every call-2 part wherever it appears, not just inside the
		// protected message; call-1 has no tools filter to save it and is
		// pruned everywhere, leaving its two messages empty and dropped.
		if len(out) != 2 {
			t.Fatalf("expected the two call-2 messages to survive, got %d: %+v", len(out), out)
		}
		for _, msg := range out {
			for _, part := range msg.Content {
				toolCallID, _, ok := isToolCallPart(part)
				if ok && toolCallID != "call-2" {
					t.Errorf("expected only call-2 parts to survive, got %+v", part)
				}
			}
		}
	})

	t.Run("N=0 behaves exactly like all (TS #21732)", func(t *testing.T) {
		zero, err := PruneModelMessages(messages, PruneModelMessagesOptions{
			ToolCalls:     []PruneToolCallsRule{{Type: "before-last-0-messages"}},
			EmptyMessages: "keep",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		all, err := PruneModelMessages(messages, PruneModelMessagesOptions{
			ToolCalls:     []PruneToolCallsRule{{Type: "all"}},
			EmptyMessages: "keep",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(zero, all) {
			t.Fatalf("before-last-0-messages = %+v, want same as all = %+v", zero, all)
		}
		// Every tool-related part across all four messages is pruned, since
		// no message is protected and the kept-ID scan contributes nothing.
		for i, msg := range zero {
			for _, part := range msg.Content {
				if _, _, ok := isToolCallPart(part); ok {
					t.Errorf("message %d: expected no tool-call parts to survive, got %+v", i, part)
				}
			}
		}
	})
}

// Go-only extension: the legacy top-level Message.ToolCalls field is pruned
// with the same rule as content parts.
func TestPruneModelMessages_PrunesLegacyToolCallsField(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			ToolCalls: []types.ToolCall{
				{ID: "call-1", ToolName: "weather-1"},
				{ID: "call-2", ToolName: "weather-2"},
			},
		},
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{
		ToolCalls: []PruneToolCallsRule{{Type: "all", Tools: []string{"weather-2"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out[0].ToolCalls) != 1 || out[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("expected only call-1 to remain in ToolCalls, got %+v", out[0].ToolCalls)
	}
	if len(messages[0].ToolCalls) != 2 {
		t.Errorf("input ToolCalls must not be mutated, got %+v", messages[0].ToolCalls)
	}
}

func TestPruneModelMessages_EmptyMessagesKeep(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		userMsg("hi"),
		assistantMsg(types.ReasoningContent{Text: "thinking"}),
	}

	out, err := PruneModelMessages(messages, PruneModelMessagesOptions{
		Reasoning:     "all",
		EmptyMessages: "keep",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected the now-empty message to be kept, got %d messages", len(out))
	}
	if len(out[1].Content) != 0 {
		t.Errorf("expected an empty content slice, got %+v", out[1].Content)
	}
}

func TestPruneModelMessages_InvalidRuleType(t *testing.T) {
	t.Parallel()

	_, err := PruneModelMessages([]types.Message{userMsg("hi")}, PruneModelMessagesOptions{
		ToolCalls: []PruneToolCallsRule{{Type: "bogus"}},
	})
	if err == nil {
		t.Fatal("expected an error for an invalid toolCalls rule type")
	}
}

func TestPruneModelMessages_InvalidReasoningValue(t *testing.T) {
	t.Parallel()

	_, err := PruneModelMessages([]types.Message{userMsg("hi")}, PruneModelMessagesOptions{
		Reasoning: "bogus",
	})
	if err == nil {
		t.Fatal("expected an error for an invalid reasoning value")
	}
}
