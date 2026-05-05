package ai

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// StopCondition evaluates after each step containing tool results.
// Returns a reason string to stop, or empty string to continue.
type StopCondition func(state StopConditionState) string

// StopConditionState contains the accumulated state passed to stop conditions.
type StopConditionState struct {
	// Steps completed so far (the step that just finished is the last element).
	Steps []types.StepResult

	// Full message history including the latest tool-result messages.
	Messages []types.Message

	// Accumulated token usage across all steps.
	Usage types.Usage
}

// IsStepCount returns a StopCondition that stops the loop when exactly n steps
// have completed. This mirrors the TypeScript SDK's isStepCount helper.
func IsStepCount(n int) StopCondition {
	return func(state StopConditionState) string {
		if len(state.Steps) == n {
			return fmt.Sprintf("maximum number of steps (%d) reached", n)
		}
		return ""
	}
}

// StepCountIs returns a StopCondition that stops the loop when exactly n steps
// have completed.
//
// Deprecated: use IsStepCount.
func StepCountIs(n int) StopCondition {
	return IsStepCount(n)
}

// IsLoopFinished returns a StopCondition that never stops the loop. This lets
// the loop continue until a natural termination condition is reached.
func IsLoopFinished() StopCondition {
	return func(state StopConditionState) string {
		return ""
	}
}

// HasToolCall returns a StopCondition that stops when the most recent step
// contains a tool call with any of the provided names.
func HasToolCall(toolNames ...string) StopCondition {
	return func(state StopConditionState) string {
		if len(state.Steps) == 0 {
			return ""
		}
		names := make(map[string]struct{}, len(toolNames))
		for _, name := range toolNames {
			names[name] = struct{}{}
		}
		lastStep := state.Steps[len(state.Steps)-1]
		for _, toolCall := range lastStep.ToolCalls {
			if _, ok := names[toolCall.ToolName]; ok {
				return fmt.Sprintf("tool '%s' was called", toolCall.ToolName)
			}
		}
		return ""
	}
}

// EvaluateStopConditions runs every condition, then returns the first non-empty
// reason, or empty string if none triggered.
//
// All conditions are always evaluated before checking results. This matches the
// TypeScript SDK's Promise.all behavior and ensures side-effectful conditions
// (e.g. recording metrics) always run regardless of their position in the slice.
func EvaluateStopConditions(conditions []StopCondition, state StopConditionState) string {
	reasons := make([]string, len(conditions))
	for i, cond := range conditions {
		reasons[i] = cond(state)
	}
	for _, reason := range reasons {
		if reason != "" {
			return reason
		}
	}
	return ""
}
