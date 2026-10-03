package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var dangerCall = types.ToolCall{ID: "call_1", ToolName: "danger", Arguments: map[string]interface{}{"x": 1.0}}

// A bare function literal used to match no case, so the tool ran without
// approval. It must behave like its named type.
func TestResolveToolApproval_UnnamedFunctionLiterals(t *testing.T) {
	ctx := context.Background()

	tool := types.Tool{Name: "danger", ToolApproval: func(ctx context.Context, input map[string]interface{}, opts types.ToolNeedsApprovalOptions) bool {
		return opts.ToolCallID == "call_1"
	}}
	got, err := resolveToolApproval(ctx, dangerCall, []types.Tool{tool}, nil, nil, nil, nil)
	if err != nil || got.Status != types.ToolApprovalStatusUserApproval {
		t.Fatalf("tool-level literal: %+v, %v; want user-approval", got, err)
	}

	legacy := types.Tool{Name: "danger", NeedsApproval: func(ctx context.Context, input map[string]interface{}) bool { return true }}
	got, err = resolveToolApproval(ctx, dangerCall, []types.Tool{legacy}, nil, nil, nil, nil)
	if err != nil || got.Status != types.ToolApprovalStatusUserApproval {
		t.Fatalf("legacy literal: %+v, %v; want user-approval", got, err)
	}

	callLevel := func(opts types.ToolApprovalOptions) types.ToolApprovalResult {
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
	}
	got, err = resolveToolApproval(ctx, dangerCall, []types.Tool{{Name: "danger"}}, nil, nil, nil, callLevel)
	if err != nil || got.Status != types.ToolApprovalStatusDenied {
		t.Fatalf("call-level literal: %+v, %v; want denied", got, err)
	}

	perTool := map[string]types.ToolApprovalValue{
		"danger": func(args map[string]interface{}, opts types.SingleToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
		},
	}
	got, err = resolveToolApproval(ctx, dangerCall, []types.Tool{{Name: "danger"}}, nil, nil, nil, perTool)
	if err != nil || got.Status != types.ToolApprovalStatusUserApproval {
		t.Fatalf("per-tool literal: %+v, %v; want user-approval", got, err)
	}
}

func TestResolveToolApproval_FailsClosedOnUnknownValues(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name         string
		tool         types.Tool
		toolApproval interface{}
		wantErr      string
	}{
		{"wrong function signature", types.Tool{Name: "danger", ToolApproval: func(input map[string]interface{}) bool { return true }}, nil, "unsupported ToolApproval value"},
		{"unsupported type", types.Tool{Name: "danger", ToolApproval: 1}, nil, "unsupported ToolApproval value"},
		{"misspelled status", types.Tool{Name: "danger", ToolApproval: "user_approval"}, nil, "unknown tool approval status"},
		{"unsupported call-level option", types.Tool{Name: "danger"}, 42, "unsupported ToolApproval option"},
		{"unsupported per-tool value", types.Tool{Name: "danger"}, map[string]interface{}{"danger": 3.5}, "unsupported tool approval value"},
	}
	for _, c := range cases {
		_, err := resolveToolApproval(ctx, dangerCall, []types.Tool{c.tool}, nil, nil, nil, c.toolApproval)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
}

// End to end: a tool configured with a bare literal must not run before the
// user approves it.
func TestGenerateText_UnnamedApprovalLiteralDoesNotExecute(t *testing.T) {
	executed := false
	tool := types.Tool{
		Name: "danger",
		ToolApproval: func(ctx context.Context, input map[string]interface{}, opts types.ToolNeedsApprovalOptions) bool {
			return true
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return "done", nil
		},
	}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{dangerCall}, FinishReason: types.FinishReasonToolCalls}, nil
		},
	}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "go",
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{IsStepCount(3)},
	})
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if executed {
		t.Fatal("tool ran without approval")
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].ApprovalStatus != types.ToolApprovalStatusUserApproval {
		t.Fatalf("tool results = %+v, want one pending user approval", result.ToolResults)
	}
}
