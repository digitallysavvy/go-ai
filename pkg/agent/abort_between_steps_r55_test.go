package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS generateText (which ToolLoopAgent runs) checks throwIfAborted at the top
// of every step after the first, so a run aborted while a tool that ignores
// the signal ran fails before the next step: no PrepareCall and no step start
// for it.
func TestGenerateAgentAbortBetweenStepsStopsBeforeNextStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "work", Arguments: map[string]interface{}{}}},
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	work := types.Tool{Name: "work", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			cancel() // ignores the abort and returns a result
			return "ok", nil
		}}
	prepareCalls, stepStarts := 0, 0
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{work},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			prepareCalls++
			return c
		},
		OnStepStart: func(int) { stepStarts++ },
	})
	_, err := a.GenerateAgent(ctx, AgentGenerateOptions{Prompt: "go"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if prepareCalls != 1 || stepStarts != 1 {
		t.Fatalf("PrepareCall calls %d, step starts %d; want 1, 1", prepareCalls, stepStarts)
	}
}

// The abort between steps reports the error through OnChainError, like the
// legacy loop's other step failures.
func TestGenerateAgentAbortBetweenStepsCallsOnChainError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "work", Arguments: map[string]interface{}{}}},
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	work := types.Tool{Name: "work", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			cancel()
			return "ok", nil
		}}
	var chainErr error
	a := NewToolLoopAgent(AgentConfig{
		Model:        model,
		Tools:        []types.Tool{work},
		OnChainError: func(err error) { chainErr = err },
	})
	if _, err := a.GenerateAgent(ctx, AgentGenerateOptions{Prompt: "go"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !errors.Is(chainErr, context.Canceled) {
		t.Fatalf("OnChainError got %v, want context.Canceled", chainErr)
	}
}
