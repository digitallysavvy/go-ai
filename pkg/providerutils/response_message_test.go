package providerutils

import (
	"encoding/json"
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
		{ID: "bad", ToolName: "lookup", Title: "Lookup", RawArguments: `{"q":`, ToolMetadata: map[string]interface{}{"source": "catalog"}, Dynamic: true, Invalid: true},
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
	if msg.ToolCalls[0].Title != "" || msg.ToolCalls[0].ToolMetadata != nil || msg.ToolCalls[0].Dynamic || msg.ToolCalls[0].Invalid {
		t.Fatalf("provider-facing tool call retained content-only fields: %+v", msg.ToolCalls[0])
	}
	if msg.ToolCalls[1].ID != "good" {
		t.Fatalf("id = %q, want good", msg.ToolCalls[1].ID)
	}
	if got := msg.ToolCalls[1].Arguments["q"]; got != "docs" {
		t.Fatalf("q = %#v, want docs", got)
	}
}

func TestConvertToResponseMessageMapsProviderMetadataToProviderOptions(t *testing.T) {
	metadata := json.RawMessage(`{"openai":{"trace":"abc"}}`)
	msg := ConvertToResponseMessage(nil, []types.ContentPart{
		types.TextContent{Text: "hello", ProviderMetadata: metadata},
		types.ReasoningContent{Text: "thinking", ProviderMetadata: metadata},
		types.FileContent{MediaType: "text/plain", Data: []byte("file"), ProviderMetadata: metadata},
		types.GeneratedFileContent{MediaType: "image/png", Data: []byte("png"), ProviderMetadata: metadata},
		types.CustomContent{Kind: "xai-citation", ProviderMetadata: metadata},
		types.ReasoningFileContent{MediaType: "text/plain", Data: []byte("reasoning"), ProviderMetadata: metadata},
		types.ToolCallContent{ToolCallID: "call-1", ToolName: "lookup", ProviderMetadata: metadata},
	})

	if len(msg.Content) != 7 {
		t.Fatalf("content len = %d, want 7", len(msg.Content))
	}
	for i, part := range msg.Content {
		var opts map[string]interface{}
		switch p := part.(type) {
		case types.TextContent:
			opts = p.ProviderOptions
			if p.ProviderMetadata != nil {
				t.Fatalf("content[%d] retained providerMetadata: %s", i, p.ProviderMetadata)
			}
		case types.ReasoningContent:
			opts = p.ProviderOptions
			if p.ProviderMetadata != nil {
				t.Fatalf("content[%d] retained providerMetadata: %s", i, p.ProviderMetadata)
			}
		case types.FileContent:
			opts = p.ProviderOptions
			if p.ProviderMetadata != nil {
				t.Fatalf("content[%d] retained providerMetadata: %s", i, p.ProviderMetadata)
			}
		case types.GeneratedFileContent:
			opts = p.ProviderOptions
			if p.ProviderMetadata != nil {
				t.Fatalf("content[%d] retained providerMetadata: %s", i, p.ProviderMetadata)
			}
		case types.CustomContent:
			opts = p.ProviderOptions
			if p.ProviderMetadata != nil {
				t.Fatalf("content[%d] retained providerMetadata: %s", i, p.ProviderMetadata)
			}
		case types.ReasoningFileContent:
			opts = p.ProviderOptions
			if p.ProviderMetadata != nil {
				t.Fatalf("content[%d] retained providerMetadata: %s", i, p.ProviderMetadata)
			}
		case types.ToolCallContent:
			opts = p.ProviderOptions
			if p.ProviderMetadata != nil {
				t.Fatalf("content[%d] retained providerMetadata: %s", i, p.ProviderMetadata)
			}
		default:
			t.Fatalf("content[%d] unexpected type %T", i, part)
		}
		if opts == nil || opts["openai"] == nil {
			t.Fatalf("content[%d] providerOptions = %+v, want forwarded metadata", i, opts)
		}
	}
}

func TestConvertToResponseMessageDerivesProviderOptionsFromMetadata(t *testing.T) {
	msg := ConvertToResponseMessage(nil, []types.ContentPart{
		types.TextContent{
			Text:             "hello",
			ProviderOptions:  map[string]interface{}{"old": "value"},
			ProviderMetadata: json.RawMessage(`{"new":{"trace":"abc"}}`),
		},
		types.ToolCallContent{
			ToolCallID:       "call-1",
			ToolName:         "lookup",
			ProviderOptions:  map[string]interface{}{"old": "value"},
			ProviderMetadata: json.RawMessage(`{"new":{"trace":"tool"}}`),
		},
	})

	if len(msg.Content) != 2 {
		t.Fatalf("content len = %d, want 2", len(msg.Content))
	}
	text := msg.Content[0].(types.TextContent)
	if text.ProviderOptions["old"] != nil || text.ProviderOptions["new"] == nil {
		t.Fatalf("text providerOptions = %+v, want metadata-derived options only", text.ProviderOptions)
	}
	call := msg.Content[1].(types.ToolCallContent)
	if call.ProviderOptions["old"] != nil || call.ProviderOptions["new"] == nil {
		t.Fatalf("tool-call providerOptions = %+v, want metadata-derived options only", call.ProviderOptions)
	}
}

func TestConvertToResponseMessageSanitizesToolCallContentInput(t *testing.T) {
	msg := ConvertToResponseMessage(nil, []types.ContentPart{
		types.ToolCallContent{
			ToolCallID:   "bad",
			ToolName:     "lookup",
			Title:        "Lookup",
			Input:        `{"q":`,
			Arguments:    map[string]interface{}{"q": "stale"},
			ToolMetadata: map[string]interface{}{"source": "catalog"},
			Dynamic:      true,
			Invalid:      true,
			Error:        "bad input",
		},
		types.ToolCallContent{ToolCallID: "good", ToolName: "lookup", Input: `{"q":"docs"}`},
	})

	if len(msg.Content) != 2 {
		t.Fatalf("content len = %d, want 2", len(msg.Content))
	}
	bad := msg.Content[0].(types.ToolCallContent)
	if len(bad.Arguments) != 0 {
		t.Fatalf("invalid content input arguments = %#v, want empty object", bad.Arguments)
	}
	if bad.Title != "" || bad.ToolMetadata != nil || bad.Dynamic || bad.Invalid || bad.Error != nil {
		t.Fatalf("provider-facing tool-call content retained content-only fields: %+v", bad)
	}
	good := msg.Content[1].(types.ToolCallContent)
	if got := good.Arguments["q"]; got != "docs" {
		t.Fatalf("q = %#v, want docs", got)
	}
	goodJSON, err := json.Marshal(good)
	if err != nil {
		t.Fatalf("marshal tool-call content: %v", err)
	}
	if got, want := string(goodJSON), `{"toolCallId":"good","toolName":"lookup","input":{"q":"docs"}}`; got != want {
		t.Fatalf("tool-call content json = %s, want %s", got, want)
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
	if part.Result != nil || part.Output == nil || part.Output.Type != types.ToolResultOutputText || part.Output.Value != "ok" {
		t.Fatalf("unexpected model output: result=%#v output=%+v", part.Result, part.Output)
	}
}

func TestConvertToResponseMessagesUsesFullStepContent(t *testing.T) {
	messages := ConvertToResponseMessages(
		[]types.ToolCall{{ID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{"city": "Tokyo"}}},
		[]types.ContentPart{
			types.TextContent{Text: "Using a tool"},
			types.SourceContent{ID: "src-1", SourceType: "url", URL: "https://example.com"},
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{"city": "Tokyo"}},
			types.ToolApprovalRequestContent{
				ApprovalID:  "call-1",
				ToolCallID:  "call-1",
				ToolCall:    types.ToolCall{ID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{"city": "Tokyo"}},
				IsAutomatic: true,
			},
			types.ToolApprovalResponseContent{
				ApprovalID: "call-1",
				ToolCall:   types.ToolCall{ID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{"city": "Tokyo"}},
				Approved:   true,
				Reason:     "safe",
			},
			types.ToolResultContent{ToolCallID: "call-1", ToolName: "lookup", Result: "sunny"},
		},
		nil,
	)

	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].Role != types.RoleAssistant {
		t.Fatalf("messages[0].Role = %s, want assistant", messages[0].Role)
	}
	if got, want := len(messages[0].Content), 3; got != want {
		t.Fatalf("assistant content len = %d, want %d: %+v", got, want, messages[0].Content)
	}
	if _, ok := messages[0].Content[0].(types.TextContent); !ok {
		t.Fatalf("assistant content[0] = %T, want TextContent", messages[0].Content[0])
	}
	if _, ok := messages[0].Content[1].(types.ToolCallContent); !ok {
		t.Fatalf("assistant content[1] = %T, want ToolCallContent", messages[0].Content[1])
	}
	req, ok := messages[0].Content[2].(types.ToolApprovalRequestContent)
	if !ok || !req.IsAutomatic {
		t.Fatalf("assistant content[2] = %#v, want automatic ToolApprovalRequestContent", messages[0].Content[2])
	}
	if req.ToolCall.ID != "" || req.ToolCall.ToolName != "" {
		t.Fatalf("provider-facing approval request retained full tool call: %+v", req)
	}
	if req.ToolCallID != "call-1" {
		t.Fatalf("approval request toolCallID = %q, want call-1", req.ToolCallID)
	}
	requestJSON, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal approval request: %v", err)
	}
	if got, want := string(requestJSON), `{"approvalId":"call-1","toolCallId":"call-1","isAutomatic":true}`; got != want {
		t.Fatalf("approval request json = %s, want %s", got, want)
	}
	if messages[1].Role != types.RoleTool {
		t.Fatalf("messages[1].Role = %s, want tool", messages[1].Role)
	}
	if got, want := len(messages[1].Content), 2; got != want {
		t.Fatalf("tool content len = %d, want %d: %+v", got, want, messages[1].Content)
	}
	resp, ok := messages[1].Content[0].(types.ToolApprovalResponseContent)
	if !ok {
		t.Fatalf("tool content[0] = %T, want ToolApprovalResponseContent", messages[1].Content[0])
	}
	if resp.ToolCall.ID != "" || resp.ToolCall.ToolName != "" || resp.ToolCallID != "" {
		t.Fatalf("provider-facing approval response retained tool call fields: %+v", resp)
	}
	responseJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal approval response: %v", err)
	}
	if got, want := string(responseJSON), `{"approvalId":"call-1","approved":true,"reason":"safe"}`; got != want {
		t.Fatalf("approval response json = %s, want %s", got, want)
	}
	tr, ok := messages[1].Content[1].(types.ToolResultContent)
	if !ok {
		t.Fatalf("tool content[1] = %T, want ToolResultContent", messages[1].Content[1])
	}
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "sunny" {
		t.Fatalf("tool result output = %+v, want text sunny", tr.Output)
	}
}

func TestConvertToResponseMessagesSynthesizesDeniedApprovalResult(t *testing.T) {
	messages := ConvertToResponseMessages(nil, []types.ContentPart{
		types.ToolApprovalResponseContent{
			ApprovalID: "approval-1",
			ToolCall:   types.ToolCall{ID: "call-1", ToolName: "danger"},
			Approved:   false,
			Reason:     "policy",
		},
	}, nil)

	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if got, want := len(messages[0].Content), 2; got != want {
		t.Fatalf("tool content len = %d, want %d: %+v", got, want, messages[0].Content)
	}
	tr, ok := messages[0].Content[1].(types.ToolResultContent)
	if !ok {
		t.Fatalf("content[1] = %T, want ToolResultContent", messages[0].Content[1])
	}
	if tr.ToolCallID != "call-1" || tr.ToolName != "danger" {
		t.Fatalf("unexpected synthesized result: %+v", tr)
	}
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputExecutionDenied || tr.Output.Reason != "policy" {
		t.Fatalf("output = %+v, want execution-denied policy", tr.Output)
	}
}

func TestConvertToResponseMessagesAppendsFallbackToolResultWithApprovalResponse(t *testing.T) {
	messages := ConvertToResponseMessages(nil, []types.ContentPart{
		types.ToolApprovalResponseContent{
			ApprovalID: "approval-1",
			ToolCall:   types.ToolCall{ID: "call-1", ToolName: "lookup"},
			Approved:   true,
			Reason:     "safe",
		},
	}, []types.ToolResult{{
		ToolCallID:       "call-1",
		ToolName:         "lookup",
		Result:           "sunny",
		ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"trace": "fallback"}},
	}})

	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if got, want := len(messages[0].Content), 2; got != want {
		t.Fatalf("tool content len = %d, want %d: %+v", got, want, messages[0].Content)
	}
	tr, ok := messages[0].Content[1].(types.ToolResultContent)
	if !ok {
		t.Fatalf("content[1] = %T, want ToolResultContent", messages[0].Content[1])
	}
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "sunny" {
		t.Fatalf("output = %+v, want text sunny", tr.Output)
	}
	if tr.ProviderOptions == nil || tr.ProviderOptions["openai"] == nil {
		t.Fatalf("provider options = %+v, want forwarded metadata", tr.ProviderOptions)
	}
}

func TestConvertToResponseMessagesKeepsProviderExecutedToolResultInAssistant(t *testing.T) {
	messages := ConvertToResponseMessages(nil, []types.ContentPart{
		types.ToolCallContent{
			ToolCallID:       "call-1",
			ToolName:         "provider_tool",
			Title:            "Provider Tool",
			Arguments:        map[string]interface{}{"query": "test"},
			ProviderExecuted: true,
		},
		types.ToolResultContent{
			ToolCallID:       "call-1",
			ToolName:         "provider_tool",
			Title:            "Provider Tool",
			Input:            map[string]interface{}{"query": "test"},
			Result:           map[string]interface{}{"value": "provider result"},
			ProviderExecuted: true,
			ProviderMetadata: json.RawMessage(`{"openai":{"trace":"result"}}`),
			ToolMetadata:     map[string]interface{}{"source": "provider"},
			Dynamic:          true,
			Preliminary:      true,
		},
	}, nil)

	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if got, want := len(messages[0].Content), 2; got != want {
		t.Fatalf("assistant content len = %d, want %d: %+v", got, want, messages[0].Content)
	}
	tr, ok := messages[0].Content[1].(types.ToolResultContent)
	if !ok {
		t.Fatalf("assistant content[1] = %T, want ToolResultContent", messages[0].Content[1])
	}
	if tr.Result != nil || tr.Output == nil || tr.Output.Type != types.ToolResultOutputJSON {
		t.Fatalf("provider tool result not normalized: result=%#v output=%+v", tr.Result, tr.Output)
	}
	if tr.Title != "" || tr.Input != nil || tr.ProviderExecuted || tr.ToolMetadata != nil || tr.Dynamic || tr.Preliminary {
		t.Fatalf("provider-facing tool result retained content-only fields: %+v", tr)
	}
	if tr.ProviderOptions["openai"] == nil {
		t.Fatalf("provider options = %+v, want forwarded metadata", tr.ProviderOptions)
	}
	if len(messages) > 1 {
		t.Fatalf("provider-executed tool result should not create tool message: %+v", messages)
	}
}

func TestConvertToResponseMessagesSerializesLocalToolErrorAsErrorText(t *testing.T) {
	messages := ConvertToResponseMessages(nil, []types.ContentPart{
		types.ToolCallContent{
			ToolCallID: "call-1",
			ToolName:   "weather",
			Arguments:  map[string]interface{}{"city": "San Francisco"},
		},
		types.ToolErrorContent{
			ToolCallID:       "call-1",
			ToolName:         "weather",
			Input:            map[string]interface{}{"city": "San Francisco"},
			Error:            "Invalid input for tool weather: JSON parsing failed",
			ProviderMetadata: json.RawMessage(`{"openai":{"trace":"local"}}`),
		},
	}, nil)

	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if got, want := len(messages[0].Content), 1; got != want {
		t.Fatalf("assistant content len = %d, want %d: %+v", got, want, messages[0].Content)
	}
	if _, ok := messages[0].Content[0].(types.ToolCallContent); !ok {
		t.Fatalf("assistant content[0] = %T, want ToolCallContent", messages[0].Content[0])
	}
	if got, want := len(messages[1].Content), 1; got != want {
		t.Fatalf("tool content len = %d, want %d: %+v", got, want, messages[1].Content)
	}
	tr, ok := messages[1].Content[0].(types.ToolResultContent)
	if !ok {
		t.Fatalf("tool content[0] = %T, want ToolResultContent", messages[1].Content[0])
	}
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText || tr.Output.Value != "Invalid input for tool weather: JSON parsing failed" {
		t.Fatalf("tool error output = %+v, want error-text", tr.Output)
	}
	if tr.ProviderOptions == nil || tr.ProviderOptions["openai"] == nil {
		t.Fatalf("provider options = %+v, want forwarded metadata", tr.ProviderOptions)
	}
}

func TestConvertToResponseMessagesSerializesProviderExecutedToolErrorAsErrorJSON(t *testing.T) {
	providerError := map[string]interface{}{"message": "provider failed"}
	messages := ConvertToResponseMessages(nil, []types.ContentPart{
		types.ToolCallContent{
			ToolCallID:       "call-1",
			ToolName:         "provider_tool",
			Arguments:        map[string]interface{}{"query": "test"},
			ProviderExecuted: true,
		},
		types.ToolErrorContent{
			ToolCallID:       "call-1",
			ToolName:         "provider_tool",
			Input:            map[string]interface{}{"query": "test"},
			Error:            providerError,
			ProviderExecuted: true,
			ProviderMetadata: json.RawMessage(`{"anthropic":{"trace":"provider"}}`),
		},
	}, nil)

	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if got, want := len(messages[0].Content), 2; got != want {
		t.Fatalf("assistant content len = %d, want %d: %+v", got, want, messages[0].Content)
	}
	tr, ok := messages[0].Content[1].(types.ToolResultContent)
	if !ok {
		t.Fatalf("assistant content[1] = %T, want ToolResultContent", messages[0].Content[1])
	}
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorJSON {
		t.Fatalf("provider tool error output = %+v, want error-json", tr.Output)
	}
	if tr.ProviderExecuted {
		t.Fatalf("provider-facing tool error retained providerExecuted: %+v", tr)
	}
	if got, ok := tr.Output.Value.(map[string]interface{}); !ok || got["message"] != "provider failed" {
		t.Fatalf("provider tool error value = %#v, want structured error", tr.Output.Value)
	}
	if tr.ProviderOptions == nil || tr.ProviderOptions["anthropic"] == nil {
		t.Fatalf("provider options = %+v, want forwarded metadata", tr.ProviderOptions)
	}
}
