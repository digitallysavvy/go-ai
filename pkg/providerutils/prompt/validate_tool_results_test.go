package prompt

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func toolCallMsg(ids ...string) types.Message {
	m := types.Message{Role: types.RoleAssistant}
	for _, id := range ids {
		m.Content = append(m.Content, types.ToolCallContent{ToolCallID: id, ToolName: "t"})
	}
	return m
}

func toolResultMsg(ids ...string) types.Message {
	m := types.Message{Role: types.RoleTool}
	for _, id := range ids {
		m.Content = append(m.Content, types.ToolResultContent{ToolCallID: id, ToolName: "t"})
	}
	return m
}

func userMsg() types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}
}

// Ports TS convert-to-language-model-prompt.validation.test.ts (TS #22449 and
// the base validation).
func TestValidateToolResults(t *testing.T) {
	tests := []struct {
		name     string
		messages []types.Message
		wantIDs  []string
		wantMsg  string
	}{
		{"resolved", []types.Message{toolCallMsg("a"), toolResultMsg("a"), userMsg()}, nil, ""},
		{"missing at end", []types.Message{toolCallMsg("a")}, []string{"a"}, "Tool result is missing for tool call a."},
		{"missing before user", []types.Message{toolCallMsg("a", "b"), toolResultMsg("a"), userMsg()}, []string{"b"}, "Tool result is missing for tool call b."},
		{"multiple missing", []types.Message{toolCallMsg("a", "b"), userMsg()}, []string{"a", "b"}, "Tool results are missing for tool calls a, b."},
		{"provider executed exempt", []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "a", ToolName: "t", ProviderExecuted: true}}}, userMsg()}, nil, ""},
		{"approval response is not a result (follow-up)", []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "a", ToolName: "t"},
				types.ToolApprovalRequestContent{ApprovalID: "x", ToolCallID: "a"}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "x", ToolCallID: "a", Approved: true}}},
			userMsg(),
		}, []string{"a"}, "Tool result is missing for tool call a."},
		{"approval response is not a result (end)", []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "a", ToolName: "t"},
				types.ToolApprovalRequestContent{ApprovalID: "x", ToolCallID: "a"}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "x", ToolCallID: "a", Approved: true}}},
		}, []string{"a"}, ""},
		// TS exempts a call only by providerExecuted on the call itself; a
		// provider-executed approval response does not exempt the call it
		// answers (r74 coordinator ruling).
		{"provider-executed approval response does not exempt the call", []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "a", ToolName: "t"}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "x", ToolCallID: "a", Approved: true, ProviderExecuted: true}}},
			userMsg(),
		}, []string{"a"}, "Tool result is missing for tool call a."},
		{"provider-executed approval response does not exempt the call (end)", []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "a", ToolName: "t"}}},
			{Role: types.RoleTool, Content: []types.ContentPart{&types.ToolApprovalResponseContent{ApprovalID: "x", ToolCallID: "a", Approved: true, ProviderExecuted: true}}},
		}, []string{"a"}, ""},
		// TS validation.test.ts "should preserve provider-executed
		// tool-approval-response": the provider-executed call is exempt.
		{"provider-executed call with provider-executed approval response", []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "call_provider_executed", ToolName: "mcp_tool", ProviderExecuted: true},
				types.ToolApprovalRequestContent{ApprovalID: "approval_provider", ToolCallID: "call_provider_executed"}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "approval_provider", Approved: true, ProviderExecuted: true}}},
		}, nil, ""},
		{"top-level ToolCalls field", []types.Message{{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "a", ToolName: "t"}}}, userMsg()}, []string{"a"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateToolResults(tt.messages)
			if tt.wantIDs == nil {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				return
			}
			var missing *types.MissingToolResultsError
			if !errors.As(err, &missing) {
				t.Fatalf("err = %v, want MissingToolResultsError", err)
			}
			if len(missing.ToolCallIDs) != len(tt.wantIDs) {
				t.Fatalf("ids = %v, want %v", missing.ToolCallIDs, tt.wantIDs)
			}
			for i, id := range tt.wantIDs {
				if missing.ToolCallIDs[i] != id {
					t.Fatalf("ids = %v, want %v", missing.ToolCallIDs, tt.wantIDs)
				}
			}
			if tt.wantMsg != "" && missing.Message != tt.wantMsg {
				t.Fatalf("message = %q, want %q", missing.Message, tt.wantMsg)
			}
		})
	}
}

func TestValidateToolResults_ProviderExecutedToolNamesAreExempt(t *testing.T) {
	messages := []types.Message{toolCallMsg("a"), userMsg()}
	if err := ValidateToolResults(messages, "t"); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if err := ValidateToolResults(messages, "other"); err == nil {
		t.Fatal("expected MissingToolResultsError")
	}
}
