package ai

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// GenerateText looks a call's tool up in the step's execution tools for its
// input callbacks (TS getOwn(stepExecutionTools, ...)): a local tool caller's
// bound tool gets them, not the model-visible caller definition.
func TestR52IntegratorInputCallbacksUseBoundCallerTool(t *testing.T) {
	var boundInput, defInput atomic.Int32
	announcement := "Available caller tools: getInventory."
	caller := localCallerTool("code_mode", func([]types.Tool) *string { return &announcement })
	caller.OnInputAvailable = func(context.Context, types.OnInputAvailableOptions) error {
		defInput.Add(1)
		return nil
	}
	bind := caller.ExperimentalToolCaller.Bind
	def := *caller.ExperimentalToolCaller
	def.Bind = func(tools []types.Tool) types.Tool {
		b := bind(tools)
		b.OnInputAvailable = func(context.Context, types.OnInputAvailableOptions) error {
			boundInput.Add(1)
			return nil
		}
		return b
	}
	caller.ExperimentalToolCaller = &def
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return toolCallResponse("call-1", "code_mode", nil), nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		StopWhen: []StopCondition{IsStepCount(1)},
		Prompt:   "Check inventory.",
		Tools: []types.Tool{
			caller,
			{
				Name:       "getInventory",
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return 42, nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{"getInventory": {"code_mode"}},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if boundInput.Load() != 1 || defInput.Load() != 0 {
		t.Fatalf("OnInputAvailable bound=%d definition=%d, want 1 and 0", boundInput.Load(), defInput.Load())
	}
}
