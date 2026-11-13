package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The ToolLoopAgent timeouts abort with the TS reasons, as GenerateText's do
// (generate-text.test.ts 8241 'should abort when step timeout expires' and
// the TS set-abort-timeout / merge-abort-signals reasons): the total and
// tool timeouts with AbortSignal.timeout's TimeoutError, the step timeout
// with "Step timeout of <ms>ms exceeded".
func TestR87cgenAgentTimeoutCauses(t *testing.T) {
	d := func(ms int) *time.Duration { v := time.Duration(ms) * time.Millisecond; return &v }
	check := func(t *testing.T, cause error, reason ai.TimeoutReason, text string) {
		t.Helper()
		var te *ai.TimeoutError
		if !errors.As(cause, &te) || te.Reason != reason || te.Error() != text || !errors.Is(cause, context.DeadlineExceeded) {
			t.Fatalf("cause = %T %v, want the %s TimeoutError %q", cause, cause, reason, text)
		}
	}
	blocking := func(cause *error) *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			<-ctx.Done()
			*cause = context.Cause(ctx)
			return nil, ctx.Err()
		}}
	}
	t.Run("total", func(t *testing.T) {
		var cause error
		_, _ = NewToolLoopAgent(AgentConfig{Model: blocking(&cause), Timeout: &ai.TimeoutConfig{Total: d(30)}}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
		check(t, cause, ai.TimeoutReasonTotal, "The operation was aborted due to timeout")
	})
	t.Run("step", func(t *testing.T) {
		var cause error
		_, _ = NewToolLoopAgent(AgentConfig{Model: blocking(&cause), Timeout: &ai.TimeoutConfig{PerStep: d(40)}}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
		check(t, cause, ai.TimeoutReasonStep, "Step timeout of 40ms exceeded")
	})
	t.Run("tool", func(t *testing.T) {
		var cause error
		n := 0
		model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			n++
			if n > 1 {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls,
				Content:   []types.ContentPart{types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Input: `{}`}},
				ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "tool1", RawArguments: `{}`}}}, nil
		}}
		tool := types.Tool{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}, Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			<-ctx.Done()
			cause = context.Cause(ctx)
			return nil, ctx.Err()
		}}
		_, _ = NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, Timeout: &ai.TimeoutConfig{ToolMs: d(30)}}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
		check(t, cause, ai.TimeoutReasonTool, "The operation was aborted due to timeout")
	})
}
