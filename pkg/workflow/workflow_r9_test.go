package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A provider-executed (MCP) tool call that the provider gated behind a
// tool-approval-request is paused for approval: TS WorkflowAgent returns it
// (providerApprovalRequests) instead of answering the call with an empty
// result and continuing.
type mcpApprovalModel struct {
	wfMockModel
}

func (m *mcpApprovalModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.calls++
	m.opts = append(m.opts, opts)
	call := types.ToolCall{ID: "mcp1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"q": "x"}, ProviderExecuted: true}
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &call},
		{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "mcp1"}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	}), nil
}

func TestWorkflowStreamPausesOnProviderApprovalRequest(t *testing.T) {
	m := &mcpApprovalModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "mcp_tool", ProviderExecuted: true}}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Err(); err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || len(st.Steps()) != 1 {
		t.Fatalf("model calls=%d steps=%d, want the loop to pause after 1", m.calls, len(st.Steps()))
	}
	found := false
	for _, p := range st.Steps()[0].Content {
		if r, ok := p.(types.ToolApprovalRequestContent); ok && r.ApprovalID == "a1" && r.ToolCall.ID == "mcp1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("step content lacks the approval request: %#v", st.Steps()[0].Content)
	}
}

func (m *mcpApprovalModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.calls++
	m.opts = append(m.opts, opts)
	call := types.ToolCall{ID: "mcp1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"q": "x"}, ProviderExecuted: true}
	return &types.GenerateResult{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{call},
		Content:      []types.ContentPart{types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "mcp1", ToolCall: call}},
	}, nil
}

func TestWorkflowGeneratePausesOnProviderApprovalRequest(t *testing.T) {
	m := &mcpApprovalModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "mcp_tool", ProviderExecuted: true}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || len(r.Steps) != 1 {
		t.Fatalf("model calls=%d steps=%d, want the loop to pause after 1", m.calls, len(r.Steps))
	}
}

// Plain ai.GenerateText stops on the provider approval request too.
func TestCoreGeneratePausesOnProviderApprovalRequest(t *testing.T) {
	m := &mcpApprovalModel{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: m, Prompt: "p", Tools: []types.Tool{{Name: "mcp_tool", ProviderExecuted: true}},
		StopWhen: []ai.StopCondition{ai.IsStepCount(5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || len(r.Steps) != 1 {
		t.Fatalf("model calls=%d steps=%d", m.calls, len(r.Steps))
	}
}
