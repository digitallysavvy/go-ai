package workflow

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type wfSiblingApprovalModel struct{ wfMockModel }

func (m *wfSiblingApprovalModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	m.calls++
	if m.calls > 1 {
		return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
	}
	var chunks []provider.StreamChunk
	for _, c := range []struct{ id, name string }{{"call-1", "action"}, {"sibling", "sibling"}, {"client", "client"}} {
		call := types.ToolCall{ID: c.id, ToolName: c.name, RawArguments: `{"value":"x"}`}
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &call})
	}
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls})
	return testutil.NewMockTextStream(chunks), nil
}

// TS workflow-agent-approval.test.ts "retains executable siblings and
// unresolved client tools with approval data" (stream mode): the step's tool
// results are written before its approval requests, the step content and the
// assistant message list the approval request after the calls and results,
// as in generate mode. Go's stream mode emitted each approval request right
// after its tool call (StreamText's order).
func TestR61WorkflowStreamApprovalRequestsFollowToolResults(t *testing.T) {
	executed := 0
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model: &wfSiblingApprovalModel{},
		Tools: []types.Tool{
			{Name: "action", ToolApproval: true, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				t.Error("approval tool executed")
				return nil, nil
			}},
			{Name: "sibling", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executed++
				return "completed", nil
			}},
			{Name: "client"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var types_ []provider.ChunkType
	stream := res.FullStream()
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		types_ = append(types_, chunk.Type)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	if executed != 1 {
		t.Fatalf("sibling executed %d times", executed)
	}
	index := func(ct provider.ChunkType) int {
		for i, c := range types_ {
			if c == ct {
				return i
			}
		}
		return -1
	}
	if r, q := index(provider.ChunkTypeToolResult), index(provider.ChunkTypeToolApprovalRequest); r < 0 || q < 0 || r > q {
		t.Fatalf("tool-result at %d, tool-approval-request at %d, want the result first: %v", r, q, types_)
	}
	if types_[len(types_)-1] != provider.ChunkTypeFinish {
		t.Fatalf("last chunk = %s, want finish", types_[len(types_)-1])
	}
	steps := res.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	var kinds []string
	for _, p := range steps[0].Content {
		kinds = append(kinds, p.ContentType())
	}
	want := []string{"tool-call", "tool-call", "tool-call", "tool-result", "tool-approval-request"}
	if len(kinds) != len(want) {
		t.Fatalf("content = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("content = %v, want %v", kinds, want)
		}
	}
	msg := steps[0].Response.Messages[0]
	last, ok := msg.Content[len(msg.Content)-1].(types.ToolApprovalRequestContent)
	if msg.Role != types.RoleAssistant || !ok || last.ApprovalID != "approval-call-1" || last.ToolCallID != "call-1" {
		t.Fatalf("assistant message = %+v", msg)
	}
}
