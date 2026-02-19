package ai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/assert"
)

func TestStepCountIs_StopsAtN(t *testing.T) {
	cond := StepCountIs(3)

	// Below threshold: should continue
	state := StopConditionState{
		Steps: make([]types.StepResult, 2),
	}
	assert.Empty(t, cond(state), "should return empty when steps < n")

	// At threshold: should stop
	state.Steps = make([]types.StepResult, 3)
	assert.NotEmpty(t, cond(state), "should return reason when steps >= n")

	// Above threshold: should stop
	state.Steps = make([]types.StepResult, 5)
	assert.NotEmpty(t, cond(state), "should return reason when steps > n")
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
