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

// StepCountIs returns a StopCondition that stops the loop after n steps.
func StepCountIs(n int) StopCondition {
	return func(state StopConditionState) string {
		if len(state.Steps) >= n {
			return fmt.Sprintf("maximum number of steps (%d) reached", n)
		}
		return ""
	}
}

// HasToolCall returns a StopCondition that stops when a specific tool is called.
func HasToolCall(toolName string) StopCondition {
	return func(state StopConditionState) string {
		if len(state.Steps) == 0 {
			return ""
		}
		lastStep := state.Steps[len(state.Steps)-1]
		for _, toolCall := range lastStep.ToolCalls {
			if toolCall.ToolName == toolName {
				return fmt.Sprintf("tool '%s' was called", toolName)
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
