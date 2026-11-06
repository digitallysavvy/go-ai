package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r60 integrator: GenerateText names approvals with InternalOptions.ApprovalID
// when it is set, and with GenerateID (TS generateId()) otherwise.
func TestR60GenerateTextInternalApprovalID(t *testing.T) {
	run := func(internal *InternalOptions) string {
		t.Helper()
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t1", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:    model,
			Prompt:   "x",
			Tools:    []types.Tool{{Name: "t1", ToolApproval: true}},
			Internal: internal,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range res.Steps[0].Content {
			if p, ok := part.(types.ToolApprovalRequestContent); ok {
				return p.ApprovalID
			}
		}
		t.Fatalf("no approval request in %v", res.Steps[0].Content)
		return ""
	}
	gen := func() string { return "gen-id" }
	if id := run(&InternalOptions{GenerateID: gen, ApprovalID: func(id string) string { return "approval-" + id }}); id != "approval-c1" {
		t.Errorf("approval ID = %q, want approval-c1", id)
	}
	if id := run(&InternalOptions{GenerateID: gen}); id != "gen-id" {
		t.Errorf("approval ID = %q, want gen-id", id)
	}
}
