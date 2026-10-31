package agent

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// createDefaultStopCondition returns the agent's default step-limit stop
// condition. It behaves like ai.IsStepCount(stepCount) and additionally logs a
// warning when the limit stops a tool loop that could otherwise continue.
// Stop conditions are only evaluated when the loop can otherwise continue, so
// logging here avoids warning when a call finishes naturally at the limit.
// Mirrors TS createDefaultStopCondition (#22329).
func createDefaultStopCondition(stepCount int) ai.StopCondition {
	limit := ai.IsStepCount(stepCount)
	return func(state ai.StopConditionState) string {
		reason := limit(state)
		if reason == "" {
			return ""
		}
		model := state.Steps[len(state.Steps)-1].Model
		ai.LogWarnings(ai.LogWarningsOptions{
			Provider: model.Provider,
			Model:    model.ModelID,
			Warnings: []types.Warning{{
				Type: "other",
				Message: fmt.Sprintf("The tool loop stopped because it reached the default stopWhen condition, IsStepCount(%d). ", stepCount) +
					"To allow more steps, set StopWhen to IsStepCount(...) with a higher limit or provide a custom stop condition. " +
					"Learn more: https://ai-sdk.dev/docs/agents/loop-control",
			}},
		})
		return reason
	}
}
