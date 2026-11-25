package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	"github.com/stretchr/testify/assert"
)

func TestIsStepCount_StopsAtExactlyN(t *testing.T) {
	cond := IsStepCount(3)

	// Below threshold: should continue
	state := StopConditionState{
		Steps: make([]types.StepResult, 2),
	}
	assert.Empty(t, cond(state), "should return empty when steps < n")

	// At threshold: should stop
	state.Steps = make([]types.StepResult, 3)
	assert.NotEmpty(t, cond(state), "should return reason when steps >= n")

	// Above threshold: should continue, matching the TypeScript SDK's exact match.
	state.Steps = make([]types.StepResult, 5)
	assert.Empty(t, cond(state), "should return empty when steps > n")
}

func TestStepCountIs_ReasonFormat(t *testing.T) {
	cond := StepCountIs(10)
	state := StopConditionState{
		Steps: make([]types.StepResult, 10),
	}
	reason := cond(state)
	assert.Equal(t, "maximum number of steps (10) reached", reason)
}

func TestStepCountIs_ZeroSteps(t *testing.T) {
	cond := StepCountIs(1)
	state := StopConditionState{
		Steps: []types.StepResult{},
	}
	assert.Empty(t, cond(state), "should continue with zero steps")
}

func TestIsLoopFinished_AlwaysContinues(t *testing.T) {
	cond := IsLoopFinished()
	assert.Empty(t, cond(StopConditionState{}))
	assert.Empty(t, cond(StopConditionState{Steps: []types.StepResult{{}}}))
}

func TestEvaluateStopConditions_FirstWins(t *testing.T) {
	first := func(state StopConditionState) string { return "first_reason" }
	second := func(state StopConditionState) string { return "second_reason" }

	conditions := []StopCondition{first, second}
	state := StopConditionState{}

	reason := EvaluateStopConditions(conditions, state)
	assert.Equal(t, "first_reason", reason)
}

func TestEvaluateStopConditions_NoneTriggered(t *testing.T) {
	noop := func(state StopConditionState) string { return "" }

	conditions := []StopCondition{noop, noop}
	state := StopConditionState{}

	reason := EvaluateStopConditions(conditions, state)
	assert.Empty(t, reason)
}

func TestEvaluateStopConditions_Empty(t *testing.T) {
	reason := EvaluateStopConditions(nil, StopConditionState{})
	assert.Empty(t, reason)
}

func TestEvaluateStopConditions_SkipsNonTriggered(t *testing.T) {
	noop := func(state StopConditionState) string { return "" }
	trigger := func(state StopConditionState) string { return "triggered" }

	conditions := []StopCondition{noop, trigger}
	state := StopConditionState{}

	reason := EvaluateStopConditions(conditions, state)
	assert.Equal(t, "triggered", reason)
}

func TestHasToolCall_MatchesToolName(t *testing.T) {
	cond := HasToolCall("finish_task")
	state := StopConditionState{
		Steps: []types.StepResult{
			{
				ToolCalls: []types.ToolCall{
					{ToolName: "search"},
				},
			},
			{
				ToolCalls: []types.ToolCall{
					{ToolName: "finish_task"},
				},
			},
		},
	}
	reason := cond(state)
	assert.NotEmpty(t, reason, "should stop when target tool is in last step")
}

func TestHasToolCall_NoMatch(t *testing.T) {
	cond := HasToolCall("finish_task")
	state := StopConditionState{
		Steps: []types.StepResult{
			{
				ToolCalls: []types.ToolCall{
					{ToolName: "search"},
					{ToolName: "calculate"},
				},
			},
		},
	}
	assert.Empty(t, cond(state), "should continue when target tool not called")
}

func TestHasToolCall_EmptySteps(t *testing.T) {
	cond := HasToolCall("finish_task")
	state := StopConditionState{
		Steps: []types.StepResult{},
	}
	assert.Empty(t, cond(state), "should continue with empty steps")
}

func TestHasToolCall_ReasonFormat(t *testing.T) {
	cond := HasToolCall("my_tool")
	state := StopConditionState{
		Steps: []types.StepResult{
			{
				ToolCalls: []types.ToolCall{
					{ToolName: "my_tool"},
				},
			},
		},
	}
	reason := cond(state)
	assert.Equal(t, "tool 'my_tool' was called", reason)
}

func TestHasToolCall_MatchesAnyToolName(t *testing.T) {
	cond := HasToolCall("search", "final_answer")
	state := StopConditionState{
		Steps: []types.StepResult{
			{
				ToolCalls: []types.ToolCall{
					{ToolName: "final_answer"},
				},
			},
		},
	}
	reason := cond(state)
	assert.Equal(t, "tool 'final_answer' was called", reason)
}

// TS evaluates stop conditions only when the loop would otherwise continue.
// A step that ends the loop on its own (no tool calls) must not call them.
func TestStopConditions_NotEvaluatedOnNaturalEnd(t *testing.T) {
	t.Parallel()

	var calls int
	cond := func(StopConditionState) string { calls++; return "" }
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x", StopWhen: []StopCondition{cond}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || res.StopReason != "" {
		t.Fatalf("generate: calls=%d reason=%q", calls, res.StopReason)
	}
	sr, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x", StopWhen: []StopCondition{cond}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sr.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || sr.StopReason() != "" {
		t.Fatalf("stream: calls=%d reason=%q", calls, sr.StopReason())
	}
}
