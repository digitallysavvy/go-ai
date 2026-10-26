package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The legacy loop fails closed like ai.GenerateText: an unknown approval
// status must not let the tool run unapproved.
func TestLegacyLoop_UnknownApprovalStatusDoesNotRunTool(t *testing.T) {
	ran := false
	tool := types.Tool{
		Name:         "danger",
		Description:  "d",
		Parameters:   map[string]interface{}{"type": "object"},
		ToolApproval: "approvd", // typo of "approved"
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			ran = true
			return "ok", nil
		},
	}
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "danger", Arguments: map[string]interface{}{}}},
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, MaxSteps: 3})
	_, _ = a.Execute(context.Background(), "go")
	if ran {
		t.Fatal("tool with an unknown approval status was executed")
	}
}

// An unsupported ToolApproval value type must not let the tool run either.
func TestLegacyLoop_UnsupportedApprovalValueDoesNotRunTool(t *testing.T) {
	for name, setting := range map[string]interface{}{
		"tool setting":    42,
		"call-level map":  nil,
		"call-level type": nil,
	} {
		t.Run(name, func(t *testing.T) {
			ran := false
			tool := types.Tool{
				Name:         "danger",
				Description:  "d",
				Parameters:   map[string]interface{}{"type": "object"},
				ToolApproval: setting,
				Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					ran = true
					return "ok", nil
				},
			}
			calls := 0
			model := &testutil.MockLanguageModel{
				DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					calls++
					if calls == 1 {
						return &types.GenerateResult{
							FinishReason: types.FinishReasonToolCalls,
							ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "danger", Arguments: map[string]interface{}{}}},
						}, nil
					}
					return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
				},
			}
			cfg := AgentConfig{Model: model, Tools: []types.Tool{tool}, MaxSteps: 3}
			switch name {
			case "call-level map":
				cfg.ToolApproval = map[string]interface{}{"danger": 42}
			case "call-level type":
				cfg.ToolApproval = 42
			}
			a := NewToolLoopAgent(cfg)
			_, _ = a.Execute(context.Background(), "go")
			if ran {
				t.Fatal("tool with an unsupported approval value was executed")
			}
		})
	}
}
