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

// TestR87cintegAgentStepTimeoutCauseReachesTools: a tool running under the
// ToolLoopAgent step timeout (no tool timeout) sees the TS step abort reason
// ("Step timeout of <ms>ms exceeded", setAbortTimeout), through the step
// context CreateTimeoutContext builds (gen 4647be0d, stream 503b979a).
func TestR87cintegAgentStepTimeoutCauseReachesTools(t *testing.T) {
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
	var cause error
	tool := types.Tool{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}, Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		<-ctx.Done()
		cause = context.Cause(ctx)
		return nil, ctx.Err()
	}}
	step := 40 * time.Millisecond
	_, _ = NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, Timeout: &ai.TimeoutConfig{PerStep: &step}}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	var te *ai.TimeoutError
	if !errors.As(cause, &te) || te.Reason != ai.TimeoutReasonStep || te.Error() != "Step timeout of 40ms exceeded" {
		t.Fatalf("tool cause = %T %v, want the step TimeoutError", cause, cause)
	}
}
