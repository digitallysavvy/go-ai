package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r60ProviderApprovalModel(requestToolCallID string) *testutil.MockLanguageModel {
	call := types.ToolCall{ID: "c1", ToolName: "mcp", Arguments: map[string]interface{}{"a": 1.0}, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}
	return &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "mcp", Input: `{"a":1}`, ProviderExecuted: true, Dynamic: true},
			types.ToolApprovalRequestContent{ApprovalID: "ap1", ToolCallID: requestToolCallID},
		}, FinishReason: types.FinishReasonToolCalls}, nil
	}}
}

// TS convertLanguageModelContent: a provider tool-approval-request part (an
// MCP server asking for approval of its call) takes the parsed tool call of
// the same response ({type, approvalId, toolCall}); one without a call throws
// ToolCallNotFoundForApprovalError before the language model call ends. Go
// GenerateText kept the request without its call (as StreamText does not)
// and accepted a request for an unknown call.
func TestR60GenerateTextProviderApprovalRequestTakesItsToolCall(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r60ProviderApprovalModel("c1"), Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var request *types.ToolApprovalRequestContent
	for _, part := range res.Steps[0].Content {
		if p, ok := part.(types.ToolApprovalRequestContent); ok {
			request = &p
		}
	}
	if request == nil {
		t.Fatalf("no approval request in %v", res.Steps[0].Content)
	}
	if request.ApprovalID != "ap1" || request.ToolCallID != "c1" || request.ToolCall.ID != "c1" || request.ToolCall.ToolName != "mcp" ||
		!request.ToolCall.ProviderExecuted || !request.ToolCall.Dynamic || request.ToolCall.Arguments["a"] != 1.0 {
		t.Fatalf("approval request = %+v, want the c1 call", *request)
	}

	var lmEnds int
	_, err = GenerateText(context.Background(), GenerateTextOptions{Model: r60ProviderApprovalModel("missing"), Prompt: "x",
		OnLanguageModelCallEnd: func(context.Context, LanguageModelCallEndEvent) { lmEnds++ }})
	if !IsToolCallNotFoundForApprovalError(err) || err.Error() != `Tool call "missing" not found for approval request "ap1".` {
		t.Fatalf("err = %v, want ToolCallNotFoundForApprovalError", err)
	}
	if lmEnds != 0 {
		t.Fatalf("OnLanguageModelCallEnd fired %d times; TS throws before it", lmEnds)
	}
}

// The same conversion runs for batch text results (TS convertGenerateResult).
func TestR60BatchProviderApprovalRequestTakesItsToolCall(t *testing.T) {
	model := r60ProviderApprovalModel("c1")
	result, _ := model.DoGenerate(context.Background(), nil)
	out, err := parseBatchToolCalls(context.Background(), result, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, part := range out.Content {
		if p, ok := part.(types.ToolApprovalRequestContent); ok {
			found = p.ToolCall.ID == "c1" && p.ToolCall.ToolName == "mcp"
		}
	}
	if !found {
		t.Fatalf("content = %+v, want the approval request with its call", out.Content)
	}
	missing, _ := r60ProviderApprovalModel("missing").DoGenerate(context.Background(), nil)
	if _, err := parseBatchToolCalls(context.Background(), missing, nil); !IsToolCallNotFoundForApprovalError(err) {
		t.Fatalf("err = %v, want ToolCallNotFoundForApprovalError", err)
	}
}
