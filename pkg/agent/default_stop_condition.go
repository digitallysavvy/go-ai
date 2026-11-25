package agent

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LogDefaultStopConditionWarning logs the warning for the default StopWhen
// condition: the tool loop stopped because it reached IsStepCount(stepCount).
// It is the part of createDefaultStopCondition that the WorkflowAgent shares
// (TS logDefaultStopConditionWarning, exported from ai/internal, f43ee39373).
// A non-nil onWarning receives the warning instead of the global logger
// (d834c5bb35).
func LogDefaultStopConditionWarning(stepCount int, provider, modelID string, onWarning ai.LogWarningsFunction) {
	ai.CreateLogWarnings(onWarning)(ai.LogWarningsOptions{
		Provider: provider,
		Model:    modelID,
		Warnings: []types.Warning{{
			Type: "other",
			Message: fmt.Sprintf("The tool loop stopped because it reached the default stopWhen condition, IsStepCount(%d). ", stepCount) +
				"To allow more steps, set StopWhen to IsStepCount(...) with a higher limit or provide a custom stop condition. " +
				"Learn more: https://ai-sdk.dev/docs/agents/loop-control",
		}},
	})
}

// createDefaultStopCondition returns the agent's default step-limit stop
// condition. It behaves like ai.IsStepCount(stepCount) and additionally logs a
// warning when the limit stops a tool loop that could otherwise continue.
// Stop conditions are only evaluated when the loop can otherwise continue, so
// logging here avoids warning when a call finishes naturally at the limit.
// Mirrors TS createDefaultStopCondition (#22329).
func createDefaultStopCondition(stepCount int, onWarning ai.LogWarningsFunction) ai.StopCondition {
	limit := ai.IsStepCount(stepCount)
	return func(state ai.StopConditionState) string {
		reason := limit(state)
		if reason == "" {
			return ""
		}
		model := state.Steps[len(state.Steps)-1].Model
		LogDefaultStopConditionWarning(stepCount, model.Provider, model.ModelID, onWarning)
		return reason
	}
}
