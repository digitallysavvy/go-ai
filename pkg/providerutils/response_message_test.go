package providerutils

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestConvertToResponseMessageDefaultsNilToolInput(t *testing.T) {
	msg := ConvertToResponseMessage([]types.ToolCall{
		{ID: "call_1", ToolName: "lookup"},
	}, []types.ContentPart{types.TextContent{Text: "Using a tool"}})

	if msg.Role != types.RoleAssistant {
		t.Fatalf("role = %s, want assistant", msg.Role)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Arguments == nil {
		t.Fatal("expected nil arguments to default to empty object")
	}
	if len(msg.ToolCalls[0].Arguments) != 0 {
		t.Fatalf("expected empty arguments, got %#v", msg.ToolCalls[0].Arguments)
	}
}

func TestConvertToResponseMessageSanitizesInvalidRawToolInput(t *testing.T) {
	msg := ConvertToResponseMessage([]types.ToolCall{
		{ID: "bad", ToolName: "lookup", RawArguments: `{"q":`},
		{ID: "good", ToolName: "lookup", RawArguments: `{"q":"docs"}`},
	}, nil)

	if len(msg.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].ID != "bad" {
		t.Fatalf("id = %q, want bad", msg.ToolCalls[0].ID)
	}
	if len(msg.ToolCalls[0].Arguments) != 0 {
		t.Fatalf("invalid raw input arguments = %#v, want empty object", msg.ToolCalls[0].Arguments)
	}
	if msg.ToolCalls[1].ID != "good" {
		t.Fatalf("id = %q, want good", msg.ToolCalls[1].ID)
	}
	if got := msg.ToolCalls[1].Arguments["q"]; got != "docs" {
		t.Fatalf("q = %#v, want docs", got)
	}
}

func TestConvertToResponseMessagesSkipsProviderExecutedToolResults(t *testing.T) {
	messages := ConvertToResponseMessages(nil, nil, []types.ToolResult{
		{ToolCallID: "local", ToolName: "localTool", Result: "ok"},
		{ToolCallID: "provider", ToolName: "web_search", Result: "already included", ProviderExecuted: true},
	})

	if len(messages) != 1 {
		t.Fatalf("expected only tool message after empty assistant is skipped, got %d", len(messages))
	}
	if len(messages[0].Content) != 1 {
		t.Fatalf("expected 1 local tool result, got %d", len(messages[0].Content))
	}
	part := messages[0].Content[0].(types.ToolResultContent)
	if part.ToolCallID != "local" {
		t.Fatalf("toolCallID = %q, want local", part.ToolCallID)
	}
}
