package core

import "github.com/digitallysavvy/go-ai/pkg/ai"

// StopCondition evaluates after each step containing tool results.
type StopCondition = ai.StopCondition

// StopConditionState contains the accumulated state passed to stop conditions.
type StopConditionState = ai.StopConditionState

// IsStepCount stops the loop when exactly n steps have completed.
func IsStepCount(n int) StopCondition {
	return ai.IsStepCount(n)
}

// StepCountIs stops the loop when exactly n steps have completed.
//
// Deprecated: use IsStepCount.
func StepCountIs(n int) StopCondition {
	return ai.IsStepCount(n)
}

// IsLoopFinished never stops the loop, allowing natural termination.
func IsLoopFinished() StopCondition {
	return ai.IsLoopFinished()
}

// HasToolCall stops when the most recent step contains any of the named tools.
func HasToolCall(toolNames ...string) StopCondition {
	return ai.HasToolCall(toolNames...)
}

// EvaluateStopConditions runs every condition and returns the first reason.
func EvaluateStopConditions(conditions []StopCondition, state StopConditionState) string {
	return ai.EvaluateStopConditions(conditions, state)
}
